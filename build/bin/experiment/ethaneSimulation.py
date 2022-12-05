from web3 import Web3
import socket
import os, binascii
import sys
import random, time
import json
from datetime import datetime
from os.path import exists
import pymysql.cursors
import csv
import multiprocessing
from multiprocessing.pool import ThreadPool as Pool

# ethereum tx data DB options
db_host = 'localhost'
db_user = 'ethereum'
db_pass = '1234' # fill in the MariaDB/MySQL password.
db_name = 'ethereum' # block 0 ~ 1,000,000
db_port = 3306 # 3306: mariadb default
conn_mariadb = lambda host, port, user, password, database: pymysql.connect(host=host, port=port, user=user, password=password, database=database, cursorclass=pymysql.cursors.DictCursor)
conn = conn_mariadb(db_host, db_port, db_user, db_pass, db_name)
cursor = conn.cursor()
conn_thread = conn_mariadb(db_host, db_port, db_user, db_pass, db_name)
cursor_thread = conn_thread.cursor() # another cursor for async thread

# simulator server IP address
SERVER_IP = "localhost"
SERVER_PORT = 8999

# simulator options
deleteDisk = True # delete disk when reset simulator or not
doStorageTrieUpdate = True # update storage tries or state trie only
stopWhenErrorOccurs = True # stop simulation when state/storage trie root is generated incorrectly
doReset = True # in Ethereum mode, do reset or not at the beginning of simulation
collectDeletedAddresses = True # in Ethanos mode, this should be true to check state correctness
doRestore = True # in Ethane/Ethanos mode, do restore inactive accounts

# file paths
blockInfosLogFilePath = "/home/jmlee/go-ethereum/simulator/blockInfos/"
trieInspectsLogFilePath = "/home/jmlee/go-ethereum/simulator/trieInspects/"
restoreListFilePath = "./restoreList/"

# maximum byte length of response from the simulator
maxResponseLen = 4096

# const for Ethane
emptyCodeHash = "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"
emptyRoot = "56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"

# open socket
client_socket = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
# connect to the server
client_socket.connect((SERVER_IP, SERVER_PORT))

# caching data from db
cache_address = {}
cache_slot = {}

# destructed addresses (needed to check Ethanos's state correctness)
deletedAddrs = {}

# read block header
def select_block_header(cursor, blocknumber):
    sql = "SELECT * FROM blocks WHERE `number`=%s;"
    cursor.execute(sql, (blocknumber,))
    result = cursor.fetchall()
    return result[0]

def select_blocks_header(cursor, startblocknumber, endblocknumber):
    sql = "SELECT `stateroot` FROM blocks WHERE `number`>=%s AND `number`<%s;"
    cursor.execute(sql, (startblocknumber, endblocknumber))
    result = cursor.fetchall()
    return result

# read tx hash of block [blocknumber]'s [txindex]^th tx
def select_tx_hash(cursor, blocknumber, txindex):
    sql = "SELECT * FROM transactions WHERE `blocknumber`=%s AND `transactionindex`=%s;"
    cursor.execute(sql, (blocknumber, txindex))
    result = cursor.fetchall()
    return result

def select_txs_hash(cursor, blocknumber):
    sql = "SELECT * FROM transactions WHERE `blocknumber`=%s;"
    cursor.execute(sql, (blocknumber, ))
    result = cursor.fetchall()
    return result

# read address of address_id
def select_address(cursor, addressid):
    # sql = "SELECT * FROM addresses WHERE `id`=%s;"
    # cursor.execute(sql, (addressid,))
    # result = cursor.fetchall()
    # return result[0]['address'].hex()
    if addressid in cache_address:
        return cache_address[addressid]
    sql = "SELECT * FROM addresses WHERE `id`=%s;"
    cursor.execute(sql, (addressid,))
    result = cursor.fetchall()
    cache_address[addressid] = result[0]['address'].hex()
    return cache_address[addressid]

# read slot of address_id
def select_slot(cursor, slotid):
    # sql = "SELECT * FROM slots WHERE `id`=%s;"
    # cursor.execute(sql, (slotid,))
    # result = cursor.fetchall()
    # return result[0]['slot'].hex()
    if slotid in cache_slot:
        return cache_slot[slotid]
    sql = "SELECT * FROM slots WHERE `id`=%s;"
    cursor.execute(sql, (slotid,))
    result = cursor.fetchall()
    cache_slot[slotid] = result[0]['slot'].hex()
    return cache_slot[slotid]

# read txs in this block from DB
def select_txs(cursor, blocknumber):
    sql = "SELECT * FROM `transactions` WHERE `blocknumber`=%s;"
    cursor.execute(sql, (blocknumber,))
    result = cursor.fetchall()
    return result

# read accounts r/w list in this block from DB
def select_account_read_write_list(cursor, blocknumber):
    sql = "SELECT * FROM `states` WHERE `blocknumber`=%s;"
    cursor.execute(sql, (blocknumber,))
    result = cursor.fetchall()
    return result

def select_account_read_write_lists(cursor, startblocknumber, endblocknumber):
    sql = "SELECT * FROM `states` WHERE `blocknumber`>=%s AND `blocknumber`<%s;"
    cursor.execute(sql, (startblocknumber, endblocknumber))
    result = cursor.fetchall()
    return result

# read storage trie slots r/w list in this block from DB
def select_slot_write_list(cursor, stateid):
    sql = "SELECT * FROM `slotlogs` WHERE `stateid`=%s;"
    cursor.execute(sql, (stateid,))
    result = cursor.fetchall()
    return result

def select_slot_write_lists(cursor, startstateid, endstateid):
    sql = "SELECT * FROM `slotlogs` WHERE `stateid`>=%s AND `stateid`<%s;"
    cursor.execute(sql, (startstateid, endstateid))
    result = cursor.fetchall()
    slots = {}
    for i in result:
        if i['stateid'] not in slots:
            slots[i['stateid']] = []
        slots[i['stateid']].append(i)
    return slots

# read state & storage r/w list
def select_state_and_storage_list(cursor, startblocknumber, endblocknumber):
    # print("select_state_and_storage_list() start")
    
    # get state r/w list
    rwList = select_account_read_write_lists(cursor, startblocknumber, endblocknumber)
    
    # get storage write list
    if len(rwList) > 0 and doStorageTrieUpdate:
        slotList = select_slot_write_lists(cursor, rwList[0]['id'], rwList[-1]['id']+1)
    else:
        slotList = []

    # return all lists
    # print("select_state_and_storage_list() end -> rwList len:", len(rwList), "/ slotList len:", len(slotList))
    return rwList, slotList

# get current block number (i.e., # of flushes)
def getBlockNum():
    cmd = str("getBlockNum")
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    blockNum = int(data.decode())
    print("blockNum:", blockNum)
    return blockNum

# get current trie's root hash
def getTrieRootHash():
    cmd = str("getTrieRootHash")
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    rootHash = data.decode()
    # print("root hash:", rootHash)
    return rootHash

# print current trie (trie.Print())
def printCurrentTrie():
    cmd = str("printCurrentTrie")
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    printResult = data.decode()
    print("print result:", printResult)
    return printResult

# get number and size of all trie nodes in db
# caution: this statistics might be wrong, use inspectDatabase() to get correct values
def getDBStatistics():
    cmd = str("getDBStatistics")
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    inspectResult = data.decode().split(',')
    totalTrieNodesNum = int(inspectResult[0])
    TotalTrieNodesSize = int(inspectResult[1])
    print("getDBStatistics result -> # of total trie nodes:", '{:,}'.format(totalTrieNodesNum), "/ total db size:", '{:,}'.format(TotalTrieNodesSize), "B")
    return inspectResult

# get number and size of all trie nodes in db
def inspectDatabase():
    cmd = str("inspectDatabase")
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    inspectResult = data.decode()
    print("inspectDatabase result:", inspectResult)
    return inspectResult

# get number and size of trie nodes in current state trie
# inspectResult: root hash, total nodes num/size, fullNode num/size, shortNode num/size, leafNode num/size
def inspectTrie():
    cmd = str("inspectTrie")
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    inspectResult = data.decode().split(',')
    print("inspectTrie result ->", inspectResult)
    return inspectResult

# get number and size of trie nodes in certain state sub trie
# inspectResult: root hash, total nodes num/size, fullNode num/size, shortNode num/size, leafNode num/size
def inspectSubTrie(subTrieRoot):
    cmd = str("inspectSubTrie")
    cmd += str(",")
    cmd += subTrieRoot
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    inspectResult = data.decode().split(',')
    print("inspectTrie result ->", inspectResult)
    return inspectResult

# print and save all db stats and current trie stats
def printAllStats(logFileName):
    cmd = str("printAllStats")
    cmd += str(",")
    cmd += logFileName
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    printResult = data.decode().split(',')
    # print("print result ->", printResult)
    return printResult

def saveBlockInfos(logFileName):
    cmd = str("saveBlockInfos")
    cmd += str(",")
    cmd += logFileName
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    saveResult = data.decode()
    # print("saveResult ->", saveResult)
    return saveResult

def estimateFlushResult():
    cmd = str("estimateFlushResult")
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    estimateFlushResult = data.decode().split(',')
    incTrieNum = int(estimateFlushResult[0])
    incTrieSize = int(estimateFlushResult[1])
    incTotalNodesNum = int(estimateFlushResult[2])
    incTotalNodesSize = int(estimateFlushResult[3])
    print("estimate flush result -> # of new trie nodes:", '{:,}'.format(incTrieNum), "/ increased trie size:", '{:,}'.format(incTrieSize), "B\n", 
        "                          # of new nodes:", '{:,}'.format(incTotalNodesNum), " / increased db size:", '{:,}'.format(incTotalNodesSize), "B")
    return estimateFlushResult

# write dirty trie nodes in db
def flush():
    cmd = str("flush")
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    flushResult = data.decode().split(',')
    newTrieNodesNum = int(flushResult[0])
    newTrieNodesSize = int(flushResult[1])
    # print("flush result -> # of new trie nodes:", '{:,}'.format(newTrieNodesNum), "/ increased db size:", '{:,}'.format(newTrieNodesSize), "B")
    return flushResult

# update account with meaningless values (using monotonically increasing nonce)
def updateTrieSimple(keyString):
    cmd = str("updateTrieSimple")
    cmd += str(",")
    cmd += keyString
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    updateTrieSimpleResult = data.decode()
    # print("updateTrieSimple result:", updateTrieSimpleResult)
    return updateTrieSimpleResult

# update account with detailed values
def updateTrie(nonce, balance, root, codeHash, addr):
    cmd = str("updateTrie")
    cmd += str(",")
    cmd += str(nonce)
    cmd += str(",")
    cmd += str(balance)
    cmd += str(",")
    cmd += str(root)
    cmd += str(",")
    cmd += str(codeHash)
    cmd += str(",")
    cmd += str(addr)
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    updateResult = data.decode()
    # print("updateTrie result:", updateResult)
    return updateResult

# delete account of this address
def updateTrieDelete(addr):
    cmd = str("updateTrieDelete")
    cmd += str(",")
    cmd += str(addr)
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    deleteResult = data.decode()
    # print("updateTrieDelete result:", deleteResult)
    if collectDeletedAddresses:
        deletedAddrs[addr] = " "
    return deleteResult

# set old block as current block (for debugging)
# ex. restart simulation from safe block
# setHead(safeBlockNum)
# simulateEthereum(safeBlockNum+1, endBlockNum)
def setHead(blockNum):
    cmd = str("setHead")
    cmd += str(",")
    cmd += str(blockNum)
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    setHeadResult = data.decode()
    print("setHeadResult result:", setHeadResult)
    if setHeadResult == "fail":
        print("setHead ERROR: cannot setHead with future block")
        sys.exit()
    return setHeadResult

# delete account of this address for Ethane
def updateTrieDeleteForEthane(addr):
    cmd = str("updateTrieDeleteForEthane")
    cmd += str(",")
    cmd += str(addr)
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    deleteResult = data.decode()
    # print("updateTrieDeleteForEthane result:", deleteResult)
    return deleteResult

# update stroage trie 
def updateStorageTrie(contractAddr, slot, value):
    cmd = str("updateStorageTrie")
    cmd += str(",")
    cmd += str(contractAddr)
    cmd += str(",")
    cmd += str(slot)
    cmd += str(",")
    cmd += str(value)
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    currentStorageRoot = data.decode()
    # print("updateStorageTrie result:", currentStorageRoot)
    return currentStorageRoot

# update stroage trie for Ethane
def updateStorageTrieEthane(contractAddr, slot, value):
    cmd = str("updateStorageTrieEthane")
    cmd += str(",")
    cmd += str(contractAddr)
    cmd += str(",")
    cmd += str(slot)
    cmd += str(",")
    cmd += str(value)
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    currentStorageRoot = data.decode()
    # print("updateStorageTrieEthane result:", currentStorageRoot)
    return currentStorageRoot

# reset simulator
def reset():
    doDeleteDisk = 0
    if deleteDisk:
        doDeleteDisk = 1
    cmd = str("reset")
    cmd += str(",")
    cmd += str(doDeleteDisk)
    print("cmd:", cmd)
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    print("reset response:", data.decode())

# undo all unflushed trie updates
def rollbackUncommittedUpdates():
    cmd = str("rollbackUncommittedUpdates")
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    print("rollbacked root hash:", data.decode())

# goes back to the target block state
def rollbackToBlock(targetBlockNum):
    cmd = str("rollbackToBlock")
    cmd += str(",")
    cmd += str(targetBlockNum)
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    rollbackToBlockResult = data.decode()
    print("rollbackToBlock result:", rollbackToBlockResult)
    return rollbackToBlockResult

# generate json file representing current trie
def trieToGraph():
    cmd = str("trieToGraph")
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    rootHash = data.decode()
    print("trieToGraph result -> root hash:", rootHash)
    return rootHash

# inspect trie within range (without params, just inspect current trie)
def inspectTrieWithinRange():
    cmd = str("inspectTrieWithinRange")
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    inspectTrieWithinRangeResult = data.decode()
    print("inspectTrieWithinRangeResult result:", inspectTrieWithinRangeResult)
    return inspectTrieWithinRangeResult

# select simulation mode (0: Geth, 1: Ethane, 2: Ethanos)
def switchSimulationMode(mode):
    cmd = str("switchSimulationMode")
    cmd += str(",")
    cmd += str(mode)
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    switchSimulationModeResult = data.decode()
    print("switchSimulationModeResult result:", switchSimulationModeResult)
    return switchSimulationModeResult

# don't stop Ethereum simulation when storage trie is different from Geth's
def acceptWrongStorageTrie(option):
    cmd = str("acceptWrongStorageTrie")
    cmd += str(",")
    cmd += str(option)
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    settingResult = data.decode()
    print("settingResult result:", settingResult)
    return settingResult

# generate random address
def makeRandAddr():
    randHex = binascii.b2a_hex(os.urandom(20))
    # print("randHex:", randHex, "/ len:", len(randHex), "/ type:", type(randHex))
    return Web3.toChecksumAddress("0x" + randHex.decode('utf-8'))

# generate hash of random address
def makeRandAddrHash():
    randAddr = str(makeRandAddr())
    addrHash = Web3.toHex(Web3.keccak(hexstr=randAddr))
    return randAddr, addrHash[2:] # delete "0x"

# convert int to address
def intToAddr(num):
    intToHexString = f'{num:0>40x}'
    # print("randHex:", intToHexString, "/ len:", len(intToHexString), "/ type:", type(intToHexString))
    return Web3.toChecksumAddress("0x" + intToHexString)

# generate random hash length hex string
def makeRandHashLengthHexString():
    randBytes = binascii.b2a_hex(os.urandom(32))
    randHexString = str(randBytes.decode('UTF-8'))
    # print("random hex string:", randHexString)
    return randHexString

# convert int to hash length hex string
def intToHashLengthHexString(num):
    hexString = f'{num:0>64x}'
    # print("intToHashLengthHexString:", hexString)
    return hexString

# convert int -> address, and hash it to get addrHash
def intAddrToAddrHash(num):
    addr = str(intToAddr(num))
    addrHash = Web3.toHex(Web3.keccak(hexstr=addr))
    # print("addr:", addr)
    # print("addrHash:", addrHash)
    return addrHash[2:] # delete "0x"

# generate sample trie for test/debugging
def generateSampleTrie():
    reset()
    startTime = datetime.now()
    accNumToInsert = 10000000
    flushEpoch = 400
    maxHashToInt = int("0xffffffffffffffffffffffffffffffff", 0)
    interval = int(maxHashToInt / accNumToInsert)
    for i in range(accNumToInsert):
        # updateTrieSimple(makeRandHashLengthHexString()) # insert random keys
        updateTrieSimple(intToHashLengthHexString(i)) # insert incremental keys (Ethane)
        # updateTrieSimple(intToHashLengthHexString(i*interval)) # try to make maximum trie
        if (i+1) % flushEpoch == 0:
            print("flush! inserted", '{:,}'.format(i+1), "accounts")
            flush()
    flush()
    endTime = datetime.now()
    print("\nupdate", '{:,}'.format(accNumToInsert), "accounts / flush epoch:", flushEpoch, "/ elapsed time:", endTime - startTime)
    inspectTrie()
    getDBStatistics()

# -----------------------------------------------------

# update account with detailed values
def updateTrieForEthane(nonce, balance, root, codeHash, addr):
    cmd = str("updateTrieForEthane")
    cmd += str(",")
    cmd += str(nonce)
    cmd += str(",")
    cmd += str(balance)
    cmd += str(",")
    cmd += str(root)
    cmd += str(",")
    cmd += str(codeHash)
    cmd += str(",")
    cmd += str(addr)
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    updateResult = data.decode()
    # print("updateTrieForEthane result:", updateResult)
    return updateResult

# update account with meaningless values (using monotonically increasing nonce)
def updateTrieForEthaneSimple(addr):
    cmd = str("updateTrieForEthaneSimple")
    cmd += str(",")
    cmd += str(addr)
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    updateResult = data.decode()
    # print("updateTrieForEthaneSimple result:", updateResult)
    return updateResult

# restore all inactive accounts of given address (for Ethane)
def restoreAccountForEthane(addr):
    cmd = str("restoreAccountForEthane")
    cmd += str(",")
    cmd += str(addr)
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    restoreResult = data.decode()
    # print("restoreAccountForEthane result:", restoreResult)
    return restoreResult

# print Ethane related stats
def printEthaneState():
    cmd = str("printEthaneState")
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    printEthaneState = data.decode()
    # print("printEthaneState result:", printEthaneState)
    return printEthaneState

def getTrieLastKey():
    cmd = str("getTrieLastKey")
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    result = data.decode()
    # print("result:", result)
    return result

# set Ethane's options
def setEthaneOptions(deleteEpoch, inactivateEpoch, inactivateCriterion, fromLevel):
    cmd = str("setEthaneOptions")
    cmd += str(",")
    cmd += str(deleteEpoch)
    cmd += str(",")
    cmd += str(inactivateEpoch)
    cmd += str(",")
    cmd += str(inactivateCriterion)
    cmd += str(",")
    cmd += str(fromLevel)
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    setOptionResult = data.decode()
    # print("setOptionResult result:", setOptionResult)
    return setOptionResult

# convert Ethane's state as Ethereum's
def convertEthaneToEthereum():
    cmd = str("convertEthaneToEthereum")
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    result = data.decode()
    # print("result:", result)
    return result

# check converted Ethane's state's correctness
def checkEthaneStateCorrectness(endBlockNum):
    convertedRoot = convertEthaneToEthereum()[2:]
    header = select_blocks_header(cursor, endBlockNum, endBlockNum+1)[0]
    correctRoot = header['stateroot'].hex()
    print("convertedRoot:", convertedRoot, "/ correctRoot:", correctRoot)
    if convertedRoot == correctRoot:
        print("correct Ethane simulation!")
    else:
        print("something wrong with Ethane")

def getRestoreList(startBlockNum, endBlockNum, inactivateEpoch, inactivateCriterion, restoreListVersion):

    if not doRestore:
        return {}, 100000000, False, 0
    
    # set restore list file names
    completeRestoreFileName = "restore_list_" + str(startBlockNum) + "_" + str(endBlockNum) \
        + "_"  + str(inactivateEpoch) + "_" + str(inactivateCriterion) + "_complete.json"
    incompleteRestoreFileName = "restore_list_" + str(startBlockNum) + "_" + str(endBlockNum) \
        + "_"  + str(inactivateEpoch) + "_" + str(inactivateCriterion) + "_v" + str(restoreListVersion) + ".json"
    print("we need one of these restore list:")
    print("  ->", completeRestoreFileName)
    print("  ->", incompleteRestoreFileName)

    # wait for restore list
    cnt = 0
    checkInterval = 10
    print("start waiting new restore list at", datetime.now().strftime("%m-%d %H:%M:%S"))
    while True:
        if os.path.exists(restoreListFilePath + completeRestoreFileName):
            print("find complete restore list:", completeRestoreFileName)
            restoreFileName = completeRestoreFileName
            incompleteRestoreList = False
            break

        if os.path.exists(restoreListFilePath + incompleteRestoreFileName):
            print("find incomplete restore list:", incompleteRestoreFileName)
            restoreFileName = incompleteRestoreFileName
            incompleteRestoreList = True
            restoreListVersion += 1
            break

        time.sleep(checkInterval)
        cnt += checkInterval
        print("  -> cnt:", cnt, "->", datetime.now().strftime("%m-%d %H:%M:%S"), end="\r")

    # return restore list
    with open(restoreListFilePath + restoreFileName, "r") as rl_json:
        restoreList = json.load(rl_json)
        lastBlockInRestoreList = int(list(restoreList.keys())[-1])
        print("return restore list:", restoreFileName)
        print("lastBlockInRestoreList:", lastBlockInRestoreList, "/ incompleteRestoreList:", incompleteRestoreList, "/ restoreListVersion:", restoreListVersion)
        time.sleep(5) # to check replace info in console
        return restoreList, lastBlockInRestoreList, incompleteRestoreList, restoreListVersion

# -----------------------------------------------------

# update account with detailed values
def updateTrieForEthanos(nonce, balance, root, codeHash, addr):
    cmd = str("updateTrieForEthanos")
    cmd += str(",")
    cmd += str(nonce)
    cmd += str(",")
    cmd += str(balance)
    cmd += str(",")
    cmd += str(root)
    cmd += str(",")
    cmd += str(codeHash)
    cmd += str(",")
    cmd += str(addr)
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    updateResult = data.decode()
    # print("updateTrieForEthanos result:", updateResult)
    return updateResult

# restore all inactive accounts of given address (for Ethanos)
def restoreAccountForEthanos(addr):
    cmd = str("restoreAccountForEthanos")
    cmd += str(",")
    cmd += str(addr)
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    restoreResult = data.decode()
    # print("restoreAccountForEthanos result:", restoreResult)
    return restoreResult

# update stroage trie for Ethanos
def updateStorageTrieEthanos(contractAddr, slot, value):
    cmd = str("updateStorageTrieEthanos")
    cmd += str(",")
    cmd += str(contractAddr)
    cmd += str(",")
    cmd += str(slot)
    cmd += str(",")
    cmd += str(value)
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    currentStorageRoot = data.decode()
    # print("updateStorageTrieEthanos result:", currentStorageRoot)
    return currentStorageRoot

# convert Ethanos's state as Ethereum's
def convertEthanosToEthereum():
    cmd = str("convertEthanosToEthereum")
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    result = data.decode()
    # print("result:", result)
    return result

# check converted Ethanos's state's correctness
def checkEthanosStateCorrectness(endBlockNum):

    # restore all addresses
    for key in cache_address:
        restoreAccountForEthanos(cache_address[key])

    # convert state
    convertedRoot = convertEthanosToEthereum()[2:]

    # remove deleted addresses from state trie
    print("to delete addr num:", len(deletedAddrs))
    for addr in deletedAddrs:
        updateTrieDelete(addr)
    convertedRoot = getTrieRootHash()[2:]

    # compare state root
    header = select_blocks_header(cursor, endBlockNum, endBlockNum+1)[0]
    correctRoot = header['stateroot'].hex()
    print("convertedRoot:", convertedRoot, "/ correctRoot:", correctRoot)
    if convertedRoot == correctRoot:
        print("correct Ethanos simulation!")
    else:
        print("something wrong with Ethanos")

# -----------------------------------------------------

# just insert random keys
def strategy_random(flushEpoch, totalAccNumToInsert, maxAddr=0xffffffffffffffffffffffffffffffffffffffff):
    # initialize
    reset()
    updateCount = 0
    startTime = datetime.now()

    # set log file name
    logFileName = "ethane_strategy_random_" + str(flushEpoch) + "_" + str(totalAccNumToInsert) + ".txt"

    # insert random accounts
    for i in range(totalAccNumToInsert):
        randAddr = intToAddr(random.randrange(maxAddr))
        # print("randAddr:", randAddr)
        updateTrieForEthaneSimple(randAddr)
        updateCount += 1
        if updateCount % flushEpoch == 0:
            print("flush! inserted", '{:,}'.format(i+1), "accounts / elapsed time:", datetime.now()-startTime)
            flush()
    flush()

    # show final result
    print("strategy_random() finished")
    print("total elapsed time:", datetime.now()-startTime)
    print("random inserts:", totalAccNumToInsert)
    inspectTrie()
    getDBStatistics()
    printAllStats(logFileName)
    print("create log file:", logFileName)
    # printCurrentTrie()
    # trieToGraph()

def test_ethane(flushEpoch):
    # initialize
    reset()
    updateCount = 0
    startTime = datetime.now()

    addrs = [4,5,6, 1,2,3, 1,2,3, 7,8,9, 10,11,12, 13,14,15, 7,8,9, 4,5,6]
    totalAccNumToInsert = len(addrs)

    # insert random accounts
    for i in range(totalAccNumToInsert):
        # key = makeRandHashLengthHexString()
        # updateTrieSimple(key)
        randAddr = intToAddr(addrs[i])
        updateTrieForEthaneSimple(randAddr)
        # printCurrentTrie()
        updateCount += 1
        if updateCount % flushEpoch == 0:
            print("flush! inserted", '{:,}'.format(i+1), "accounts / elapsed time:", datetime.now()-startTime)
            flush()
        printCurrentTrie()
    # flush()

    # show final result
    print("strategy_random() finished")
    print("total elapsed time:", datetime.now()-startTime)
    print("random inserts:", totalAccNumToInsert)
    inspectTrie()
    getDBStatistics()
    printCurrentTrie()

# replay txs in Ethereum with original Ethereum client
def simulateEthereum(startBlockNum, endBlockNum):
    # set Ethereum's options
    switchSimulationMode(0) # 0: original geth mode

    if doStorageTrieUpdate and not stopWhenErrorOccurs:
        acceptWrongStorageTrie(1) # 1: accept
    else:
        acceptWrongStorageTrie(0) # 0: do not accept

    # initialize
    if doReset:
        reset()
    stateReadCount = 0
    stateWriteCount = 0
    storageWriteCount = 0
    initialInvalidStateBlockNum = 999999999
    initialInvalidStorageBlockNum = 999999999
    startTime = datetime.now()

    # block batch size for db query
    batchsize = 200
    # print log interval
    loginterval = 10000

    oldblocknumber = startBlockNum
    start = time.time()
    tempStart = start
    blockcount = 0

    # set log file name
    logFileName = "ethereum_simulate_" + str(startBlockNum) + "_" + str(endBlockNum) + ".txt"
    blockInfosLogFileName = "ethereum_simulate_block_infos_" + str(startBlockNum) + "_" + str(endBlockNum) + ".txt"

    rwList = []
    slotList = []
    headers = {}
    currentStorageRoot = ""
    # insert random accounts
    for blockNum in range(startBlockNum, endBlockNum+batchsize*2, batchsize):
        #print("do block", blockNum ,"\n")
        
        # get read/write list from DB
        queryResult = pool.apply_async(select_state_and_storage_list, (cursor_thread, blockNum, blockNum+batchsize,))
        # print("\nquery blocks ->", blockNum, "~", blockNum+batchsize)
        
        # replay writes
        for item in rwList:
            # print("item:", item)
            if item['blocknumber'] != oldblocknumber:
                # printCurrentTrie()
                # flush
                blockcount += 1
                flush()
                # print("flush finished -> generated block", oldblocknumber, "\n\n\n")
                
                # check current trie is made correctly
                if oldblocknumber not in headers:
                    headers = dict(zip(range(blockNum-batchsize, blockNum), select_blocks_header(cursor, blockNum-batchsize, blockNum)))
                stateRootInBlockHeader = headers[oldblocknumber]['stateroot'].hex()
                currentStateRoot = getTrieRootHash()[2:]
                if currentStateRoot != stateRootInBlockHeader:
                    if initialInvalidStateBlockNum > oldblocknumber:
                            initialInvalidStateBlockNum = oldblocknumber
                    # print("@@fail: state trie is wrong")
                    # print(currentStateRoot, "vs", stateRootInBlockHeader)
                    # print('block #{}'.format(oldblocknumber))
                    # print("initial invalid state block num:", initialInvalidStateBlockNum)
                    if stopWhenErrorOccurs:
                        sys.exit()
                else:
                    pass
                    # print("@@success")
                    # print(currentStateRoot, "vs", stateRootInBlockHeader)
                if oldblocknumber % loginterval == 0:
                    currentTime = time.time()
                    seconds = currentTime - start
                    tempSeconds = currentTime - tempStart
                    tempStart = currentTime
                    print('# Blkn: {} (avg: {:.2f}/s) (cur: {:.2f}/s) / ethereum {} ~ {} / time: {} ~ {} / port: {}'.format( \
                        oldblocknumber, blockcount/seconds, loginterval/tempSeconds, startBlockNum, f"{endBlockNum:,}", \
                        startTime.strftime("%d %H:%M:%S"), datetime.now().strftime("%d %H:%M:%S"), SERVER_PORT))
                oldblocknumber += 1
                #print("do block", oldblocknumber ,"\n")

            if item['blocknumber'] > endBlockNum:
                print("reaching end block num, stop writing")
                break

            if item['type'] % 2 == 1:
                # print("do writes in block", item['blocknumber'])
                # print("item:", item)

                addrId = item['address_id']
                addr = select_address(cursor, addrId)

                if item['type'] == 63:
                    # delete this address
                    updateTrieDelete(addr)
                    continue

                nonce = item['nonce']
                balance = item['balance']
                codeHash = emptyCodeHash
                storageRoot = emptyRoot
                if item['codehash'] != None:
                    codeHash = item['codehash'].hex()
                if item['storageroot'] != None:
                    storageRoot = item['storageroot'].hex()

                # print("in block", blockNum)
                # print("write account ->")
                # print("  addr:", addr)
                # print("  nonce:", nonce)
                # print("  balance:", balance)
                # print("  codeHash:", codeHash)
                # print("  storageRoot:", storageRoot)
                # print("\n")

                # update storage trie
                if doStorageTrieUpdate:
                    if item['id'] in slotList:
                        slotWriteList = slotList[item['id']]
                        # print("item:", item)
                        # print("txHash:", item['txhash'].hex())
                        for slotWrite in slotWriteList:
                            contractAddrId = slotWrite['address_id']
                            contractAddress = select_address(cursor, contractAddrId)

                            slotId = slotWrite['slot_id']
                            slot = select_slot(cursor, slotId)

                            slotValue = []
                            # slotValue = 0x0 # 
                            if slotWrite['slotvalue'] != None:
                                slotValue = slotWrite['slotvalue'].hex()

                            # print("item:", slotWrite)
                            # print("contractAddress:", contractAddress)
                            # print("slot:", slot)
                            # print("slotValue:", slotValue)
                            # print("\n")
                            currentStorageRoot = updateStorageTrie(contractAddress, slot, slotValue)[2:]
                            storageWriteCount += 1

                        # check current storage trie is made correctly
                        if currentStorageRoot != storageRoot:
                            # print("@@fail: storage trie is wrong")
                            # print("blocknum:", oldblocknumber)
                            # print("txindex:", item['txindex'])
                            # print("tx hash:", select_tx_hash(cursor, blockNum, item['txindex']))
                            # print("item:", item)
                            # print("  addr:", addr)
                            # print("  nonce:", nonce)
                            # print("  balance:", balance)
                            # print("  codeHash:", codeHash)
                            # print("  storageRoot:", storageRoot)
                            # print(currentStorageRoot, "vs", storageRoot)

                            if initialInvalidStorageBlockNum > oldblocknumber:
                                initialInvalidStorageBlockNum = oldblocknumber
                            # print("initial invalid storage block num:", initialInvalidStorageBlockNum)
                            
                            if stopWhenErrorOccurs:
                                sys.exit()
                
                # update state trie
                updateTrie(nonce, balance, storageRoot, codeHash, addr)
                stateWriteCount += 1
            else:
                stateReadCount += 1
        
        # fill queried data which is gained asynchronously
        rwList, slotList = queryResult.get()
        # print("get queried blocks ->", blockNum, "~", blockNum+batchsize)

    # show final result
    print("simulateEthereum() finished -> from block", startBlockNum, "to", endBlockNum)
    print("total elapsed time:", datetime.now()-startTime)
    print("state reads:", stateReadCount,"/ state writes:", stateWriteCount, "/ storage writes:", storageWriteCount)
    print(stateReadCount, stateWriteCount, storageWriteCount)
    print("initial invalid state block num:", initialInvalidStateBlockNum)
    print("initial invalid storage block num:", initialInvalidStorageBlockNum)

    # inspectTrie()
    # printCurrentTrie()
    # inspectTrie()
    getDBStatistics()
    saveBlockInfos(blockInfosLogFileName)
    print("create log file:", blockInfosLogFileName)
    # printAllStats(logFileName)
    # print("create log file:", logFileName)

# replay txs in Ethereum with Ethane client
def simulateEthane(startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion, fromLevel):
    # set Ethane's options
    switchSimulationMode(1) # 1: Ethane mode
    setEthaneOptions(deleteEpoch, inactivateEpoch, inactivateCriterion, fromLevel)

    # set log file name
    logFileName = "ethane_simulate_" + str(startBlockNum) + "_" + str(endBlockNum) \
        + "_" + str(deleteEpoch) + "_" + str(inactivateEpoch) + "_" + str(inactivateCriterion) + ".txt"
    blockInfosLogFileName = "ethane_simulate_block_infos_" + str(startBlockNum) + "_" + str(endBlockNum) \
        + "_" + str(deleteEpoch) + "_" + str(inactivateEpoch) + "_" + str(inactivateCriterion) + ".txt"

    # get restore list
    restoreListVersion = 0
    restoreList, lastBlockInRestoreList, incompleteRestoreList, restoreListVersion = getRestoreList(startBlockNum, endBlockNum, inactivateEpoch, inactivateCriterion, restoreListVersion)

    # initialize
    if doReset:
        reset()
    updateCount = 0
    stateReadCount = 0
    stateWriteCount = 0
    storageWriteCount = 0
    restoreCount = 0
    startTime = datetime.now()

    # block batch size for db query
    batchsize = 200
    # print log interval
    loginterval = 10000

    oldblocknumber = startBlockNum
    start = time.time()
    tempStart = start
    blockcount = 0

    rwList = []
    slotList = []
    currentStorageRoot = ""
    # insert random accounts
    for blockNum in range(startBlockNum, endBlockNum+batchsize*2, batchsize):
        #print("do block", blockNum ,"\n")

        # get read/write list from DB
        queryResult = pool.apply_async(select_state_and_storage_list, (cursor_thread, blockNum, blockNum+batchsize,))
        # print("\nquery blocks ->", blockNum, "~", blockNum+batchsize)
        
        # replay writes
        for item in rwList:
            # print("item:", item)
            if item['blocknumber'] != oldblocknumber:
                # printCurrentTrie()
                # flush
                blockcount += 1
                flush()
                # print("flush finished -> generated block", oldblocknumber, "\n\n\n")

                if oldblocknumber % loginterval == 0:
                    currentTime = time.time()
                    seconds = currentTime - start
                    tempSeconds = currentTime - tempStart
                    tempStart = currentTime
                    print('# Blkn: {} (avg: {:.2f}/s) (cur: {:.2f}/s) / ethane {} ~ {} ({}, {}, {}) / time: {} ~ {} / port: {}'.format( \
                        oldblocknumber, blockcount/seconds, loginterval/tempSeconds, startBlockNum, f"{endBlockNum:,}", \
                        f"{deleteEpoch:,}", f"{inactivateEpoch:,}", f"{inactivateCriterion:,}", \
                        startTime.strftime("%d %H:%M:%S"), datetime.now().strftime("%d %H:%M:%S"), SERVER_PORT))
                oldblocknumber += 1
                #print("do block", oldblocknumber ,"\n")

                # restore accounts
                blockNumKey = str(item['blocknumber'])
                if blockNumKey in restoreList:
                    # print("restore list in block", blockNumKey, ":", restoreList[blockNumKey])
                    for addr in restoreList[blockNumKey]:
                        # print("restore addr:", addr)
                        restoreAccountForEthane(addr)
                        restoreCount += 1

                # replace incomplete restore list
                if int(blockNumKey) == lastBlockInRestoreList and incompleteRestoreList:
                    print("replace incomplete restore list")
                    restoreList, lastBlockInRestoreList, incompleteRestoreList, restoreListVersion = getRestoreList(startBlockNum, endBlockNum, inactivateEpoch, inactivateCriterion, restoreListVersion)

            if item['blocknumber'] > endBlockNum:
                print("reaching end block num, stop writing")
                break

            if item['type'] % 2 == 1:
                # print("do writes in block", item['blocknumber'])
                # print("item:", item)

                addrId = item['address_id']
                addr = select_address(cursor, addrId)

                if item['type'] == 63:
                    # delete this address
                    updateTrieDeleteForEthane(addr)
                    continue

                nonce = item['nonce']
                balance = item['balance']
                codeHash = emptyCodeHash
                storageRoot = emptyRoot
                if item['codehash'] != None:
                    codeHash = item['codehash'].hex()
                if item['storageroot'] != None:
                    storageRoot = item['storageroot'].hex()

                # print("in block", blockNum)
                # print("write account ->")
                # print("  addr:", addr)
                # print("  nonce:", nonce)
                # print("  balance:", balance)
                # print("  codeHash:", codeHash)
                # print("  storageRoot:", storageRoot)
                # print("\n")

                # update storage trie
                if doStorageTrieUpdate:
                    if item['id'] in slotList:
                        slotWriteList = slotList[item['id']]
                        # print("item:", item)
                        # print("txHash:", item['txhash'].hex())
                        for slotWrite in slotWriteList:
                            contractAddrId = slotWrite['address_id']
                            contractAddress = select_address(cursor, contractAddrId)

                            slotId = slotWrite['slot_id']
                            slot = select_slot(cursor, slotId)

                            slotValue = []
                            # slotValue = 0x0 # 
                            if slotWrite['slotvalue'] != None:
                                slotValue = slotWrite['slotvalue'].hex()

                            # print("item:", slotWrite)
                            # print("contractAddress:", contractAddress)
                            # print("slot:", slot)
                            # print("slotValue:", slotValue)
                            # print("\n")
                            currentStorageRoot = updateStorageTrieEthane(contractAddress, slot, slotValue)
                            storageWriteCount += 1

                        # check current storage trie is made correctly
                        if currentStorageRoot[2:] != storageRoot:
                            print("@@fail: storage trie is wrong")
                            print("blocknum:", oldblocknumber)
                            # print("txindex:", item['txindex'])
                            # print("tx hash:", select_tx_hash(cursor, blockNum, item['txindex']))
                            print("item:", item)
                            print("  addr:", addr)
                            print("  nonce:", nonce)
                            print("  balance:", balance)
                            print("  codeHash:", codeHash)
                            print("  storageRoot:", storageRoot)
                            print(currentStorageRoot[2:], "vs", storageRoot)
                            if stopWhenErrorOccurs:
                                sys.exit()
                
                # update state trie
                updateTrieForEthane(nonce, balance, storageRoot, codeHash, addr)
                stateWriteCount += 1
            else:
                stateReadCount += 1
        
        # fill queried data which is gained asynchronously
        rwList, slotList = queryResult.get()
        # print("get queried blocks ->", blockNum, "~", blockNum+batchsize)

    # show final result
    print("simulateEthane() finished -> from block", startBlockNum, "to", endBlockNum)
    print("total elapsed time:", datetime.now()-startTime)
    print("state reads:", stateReadCount,"/ state writes:", stateWriteCount, "/ storage writes:", storageWriteCount)
    print("restored accounts:", restoreCount)
    print(stateReadCount, stateWriteCount, storageWriteCount)
    # inspectTrie()
    # printCurrentTrie()
    printEthaneState()
    getDBStatistics()
    # inspectDatabase()
    saveBlockInfos(blockInfosLogFileName)
    print("create log file:", blockInfosLogFileName)
    # printAllStats(logFileName)
    # print("create log file:", logFileName)

# replay txs in Ethereum with Ethanos client (in Ethanos, inactivateEpoch = inactivateCriterion)
def simulateEthanos(startBlockNum, endBlockNum, inactivateCriterion, fromLevel):
    
    # set Ethanos's options
    switchSimulationMode(2) # 2: Ethanos mode
    setEthaneOptions(inactivateCriterion, inactivateCriterion, inactivateCriterion, fromLevel)

    # set log file name
    logFileName = "ethanos_simulate_" + str(startBlockNum) + "_" + str(endBlockNum) + "_" + str(inactivateCriterion) + ".txt"
    blockInfosLogFileName = "ethanos_simulate_block_infos_" + str(startBlockNum) + "_" + str(endBlockNum) + "_" + str(inactivateCriterion) + ".txt"

    # get restore list exist
    restoreListVersion = 0
    restoreList, lastBlockInRestoreList, incompleteRestoreList, restoreListVersion = getRestoreList(startBlockNum, endBlockNum, inactivateCriterion, inactivateCriterion, restoreListVersion)

    # initialize
    if doReset:
        reset()
    updateCount = 0
    stateReadCount = 0
    stateWriteCount = 0
    storageWriteCount = 0
    restoreCount = 0
    startTime = datetime.now()

    # block batch size for db query
    batchsize = 200
    # print log interval
    loginterval = 10000

    oldblocknumber = startBlockNum
    start = time.time()
    tempStart = start
    blockcount = 0

    rwList = []
    slotList = []
    currentStorageRoot = ""
    # insert random accounts
    for blockNum in range(startBlockNum, endBlockNum+batchsize*2, batchsize):
        #print("do block", blockNum ,"\n")
        
        # get read/write list from DB
        queryResult = pool.apply_async(select_state_and_storage_list, (cursor_thread, blockNum, blockNum+batchsize,))
        # print("\nquery blocks ->", blockNum, "~", blockNum+batchsize)
        
        # replay writes
        for item in rwList:
            # print("item:", item)
            if item['blocknumber'] != oldblocknumber:
                # printCurrentTrie()
                # flush
                blockcount += 1
                flush()
                # print("flush finished -> generated block", oldblocknumber, "\n\n\n")

                if oldblocknumber % loginterval == 0:
                    currentTime = time.time()
                    seconds = currentTime - start
                    tempSeconds = currentTime - tempStart
                    tempStart = currentTime
                    print('# Blkn: {} (avg: {:.2f}/s) (cur: {:.2f}/s), ethanos {} ~ {} ({}), time: {} ~ {} / port: {}'.format( \
                        oldblocknumber, blockcount/seconds, loginterval/tempSeconds, startBlockNum, f"{endBlockNum:,}", \
                        f"{inactivateCriterion:,}", startTime.strftime("%d %H:%M:%S"), datetime.now().strftime("%d %H:%M:%S"), SERVER_PORT))
                oldblocknumber += 1
                #print("do block", oldblocknumber ,"\n")

                # restore accounts
                blockNumKey = str(item['blocknumber'])
                if blockNumKey in restoreList:
                    # print("restore list in block", blockNumKey, ":", restoreList[blockNumKey])
                    for addr in restoreList[blockNumKey]:
                        # print("restore addr:", addr)
                        restoreAccountForEthanos(addr)
                        restoreCount += 1

                # replace incomplete restore list
                if int(blockNumKey) == lastBlockInRestoreList and incompleteRestoreList:
                    print("replace incomplete restore list")
                    restoreList, lastBlockInRestoreList, incompleteRestoreList, restoreListVersion = getRestoreList(startBlockNum, endBlockNum, inactivateCriterion, inactivateCriterion, restoreListVersion)

            if item['blocknumber'] > endBlockNum:
                print("reaching end block num, stop writing")
                break

            if item['type'] % 2 == 1:
                # print("do writes in block", item['blocknumber'])
                # print("item:", item)

                addrId = item['address_id']
                addr = select_address(cursor, addrId)

                if item['type'] == 63:
                    # delete this address
                    updateTrieDelete(addr)
                    continue

                nonce = item['nonce']
                balance = item['balance']
                codeHash = emptyCodeHash
                storageRoot = emptyRoot
                if item['codehash'] != None:
                    codeHash = item['codehash'].hex()
                if item['storageroot'] != None:
                    storageRoot = item['storageroot'].hex()

                # print("in block", blockNum)
                # print("write account ->")
                # print("  addr:", addr)
                # print("  nonce:", nonce)
                # print("  balance:", balance)
                # print("  codeHash:", codeHash)
                # print("  storageRoot:", storageRoot)
                # print("\n")

                # update storage trie
                if doStorageTrieUpdate:
                    if item['id'] in slotList:
                        slotWriteList = slotList[item['id']]
                        # print("item:", item)
                        # print("txHash:", item['txhash'].hex())
                        for slotWrite in slotWriteList:
                            contractAddrId = slotWrite['address_id']
                            contractAddress = select_address(cursor, contractAddrId)

                            slotId = slotWrite['slot_id']
                            slot = select_slot(cursor, slotId)

                            slotValue = []
                            # slotValue = 0x0 # 
                            if slotWrite['slotvalue'] != None:
                                slotValue = slotWrite['slotvalue'].hex()

                            # print("item:", slotWrite)
                            # print("contractAddress:", contractAddress)
                            # print("slot:", slot)
                            # print("slotValue:", slotValue)
                            # print("\n")
                            currentStorageRoot = updateStorageTrieEthanos(contractAddress, slot, slotValue)
                            storageWriteCount += 1

                        # check current storage trie is made correctly
                        if currentStorageRoot[2:] != storageRoot:
                            print("@@fail: storage trie is wrong")
                            print("blocknum:", oldblocknumber)
                            # print("txindex:", item['txindex'])
                            # print("tx hash:", select_tx_hash(cursor, blockNum, item['txindex']))
                            print("item:", item)
                            print("  addr:", addr)
                            print("  nonce:", nonce)
                            print("  balance:", balance)
                            print("  codeHash:", codeHash)
                            print("  storageRoot:", storageRoot)
                            print(currentStorageRoot[2:], "vs", storageRoot)
                            if stopWhenErrorOccurs:
                                sys.exit()

                # collect destructed addresses (to check state correctness later)
                if collectDeletedAddresses and addr in deletedAddrs:
                    deletedAddrs.pop(addr, None)
                
                # update state trie
                updateTrieForEthanos(nonce, balance, storageRoot, codeHash, addr)
                stateWriteCount += 1
            else:
                stateReadCount += 1
        
        # fill queried data which is gained asynchronously
        rwList, slotList = queryResult.get()
        # print("get queried blocks ->", blockNum, "~", blockNum+batchsize)

    # show final result
    print("simulateEthanos() finished -> from block", startBlockNum, "to", endBlockNum)
    print("total elapsed time:", datetime.now()-startTime)
    print("state reads:", stateReadCount,"/ state writes:", stateWriteCount, "/ storage writes:", storageWriteCount)
    print("restored accounts:", restoreCount)
    print(stateReadCount, stateWriteCount, storageWriteCount)
    # inspectTrie()
    # printCurrentTrie()
    getDBStatistics()
    saveBlockInfos(blockInfosLogFileName)
    print("create log file:", blockInfosLogFileName)
    # inspectDatabase()
    # printAllStats(logFileName)
    # print("create log file:", logFileName)

# inspect tries after simulation for ethereum
def inspectTriesEthereum(startBlockNum, endBlockNum, inactivateCriterion):

    print("inspectTriesEthereum() start")
    totalStartTime = datetime.now()

    delimiter = " "

    blockInfosLogFileName = "ethereum_simulate_block_infos_" + str(startBlockNum) + "_" + str(endBlockNum) + ".txt"
    trieInspectLogFileName = "ethereum_simulate_trie_inspects_" + str(startBlockNum) + "_" + str(endBlockNum) + "_" + str(inactivateCriterion) + ".txt"

    blockInfosFile = open(blockInfosLogFilePath+blockInfosLogFileName, 'r')
    if os.path.exists(trieInspectsLogFilePath+trieInspectLogFileName):
        os.remove(trieInspectsLogFilePath+trieInspectLogFileName)
    trieInspectFile = open(trieInspectsLogFilePath+trieInspectLogFileName, 'a')
    rdr = csv.reader(blockInfosFile)
    blockNum = 0
    for line in rdr:
        if len(line) == 0:
            continue

        # parsing
        params = line[0].split(delimiter)[:-1]
        blockNum = int(params[2])
        # print("blockNum:", blockNum)

        # inspect trie
        if (blockNum+1) % inactivateCriterion == 0:
            activeTrieRoot = params[0]
            startTime = datetime.now()
            nodeStat = inspectSubTrie(activeTrieRoot)[0]
            endTime = datetime.now()
            print("at block", blockNum, ", node stat:", nodeStat, "elapsed time:", endTime-startTime)

            # save result
            log = str(blockNum) + delimiter + activeTrieRoot + delimiter + nodeStat + "\n"
            trieInspectFile.write(log)
    
    trieInspectFile.close()
    print("create log file:", trieInspectLogFileName)
    print("total trie inspect time:", datetime.now()-totalStartTime)

# inspect tries after simulation for ethane
def inspectTriesEthane(startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion):

    print("inspectTriesEthane() start")
    totalStartTime = datetime.now()

    delimiter = " "

    blockInfosLogFileName = "ethane_simulate_block_infos_" + str(startBlockNum) + "_" + str(endBlockNum) \
        + "_" + str(deleteEpoch) + "_" + str(inactivateEpoch) + "_" + str(inactivateCriterion) + ".txt"
    trieInspectLogFileName = "ethane_simulate_trie_inspects_" + str(startBlockNum) + "_" + str(endBlockNum) \
        + "_" + str(deleteEpoch) + "_" + str(inactivateEpoch) + "_" + str(inactivateCriterion) + ".txt"
    
    blockInfosFile = open(blockInfosLogFilePath+blockInfosLogFileName, 'r')
    if os.path.exists(trieInspectsLogFilePath+trieInspectLogFileName):
        os.remove(trieInspectsLogFilePath+trieInspectLogFileName)
    trieInspectFile = open(trieInspectsLogFilePath+trieInspectLogFileName, 'a')
    rdr = csv.reader(blockInfosFile)
    blockNum = 0
    zeroNodeStat = "0 0 0 0 0 0 0 0 0 0 0 "
    for line in rdr:
        if len(line) == 0:
            continue

        # parsing
        params = line[0].split(delimiter)[:-1]
        blockNum = int(params[2])
        # print("blockNum:", blockNum)

        # inspect max active trie (before delete/inactivate)
        if (blockNum+2) % inactivateCriterion == 0:
            activeTrieRoot = params[0]
            inactiveTrieRoot = params[1]

            startTime = datetime.now()
            activeNodeStat = inspectSubTrie(activeTrieRoot)[0]
            endTime = datetime.now()
            print("at block", blockNum, ", active node stat:", activeNodeStat, "elapsed time:", endTime-startTime)

            inactiveNodeStat = zeroNodeStat

            # save result
            log = str(blockNum) + delimiter + activeTrieRoot + delimiter + activeNodeStat + inactiveTrieRoot + delimiter + inactiveNodeStat + "\n"
            trieInspectFile.write(log)

        # inspect max inactive trie (after inactivate)
        if (blockNum+1) % inactivateCriterion == 0:
            activeTrieRoot = params[0]
            inactiveTrieRoot = params[1]

            activeNodeStat = zeroNodeStat

            startTime = datetime.now()
            inactiveNodeStat = inspectSubTrie(inactiveTrieRoot)[0]
            endTime = datetime.now()
            print("at block", blockNum, ", inactive node stat:", inactiveNodeStat, "elapsed time:", endTime-startTime)

            # save result
            log = str(blockNum) + delimiter + activeTrieRoot + delimiter + activeNodeStat + inactiveTrieRoot + delimiter + inactiveNodeStat + "\n"
            trieInspectFile.write(log)

    trieInspectFile.close()
    print("create log file:", trieInspectLogFileName)
    print("total trie inspect time:", datetime.now()-totalStartTime)

# inspect tries after simulation for Ethanos (same as inspectTriesEthereum())
def inspectTriesEthanos(startBlockNum, endBlockNum, inactivateCriterion):

    print("inspectTriesEthanos() start")
    totalStartTime = datetime.now()

    delimiter = " "

    blockInfosLogFileName = "ethanos_simulate_block_infos_" + str(startBlockNum) + "_" + str(endBlockNum) + "_" + str(inactivateCriterion) +".txt"
    trieInspectLogFileName = "ethanos_simulate_trie_inspects_" + str(startBlockNum) + "_" + str(endBlockNum) + "_" + str(inactivateCriterion) + ".txt"

    blockInfosFile = open(blockInfosLogFilePath+blockInfosLogFileName, 'r')
    if os.path.exists(trieInspectsLogFilePath+trieInspectLogFileName):
        os.remove(trieInspectsLogFilePath+trieInspectLogFileName)
    trieInspectFile = open(trieInspectsLogFilePath+trieInspectLogFileName, 'a')
    rdr = csv.reader(blockInfosFile)
    blockNum = 0
    for line in rdr:
        if len(line) == 0:
            continue

        # parsing
        params = line[0].split(delimiter)[:-1]
        blockNum = int(params[2])
        # print("blockNum:", blockNum)

        # inspect trie
        if (blockNum+1) % inactivateCriterion == 0:
            activeTrieRoot = params[0]
            startTime = datetime.now()
            nodeStat = inspectSubTrie(activeTrieRoot)[0]
            endTime = datetime.now()
            print("at block", blockNum, ", node stat:", nodeStat, "elapsed time:", endTime-startTime)

            # save result
            log = str(blockNum) + delimiter + activeTrieRoot + delimiter + nodeStat + "\n"
            trieInspectFile.write(log)
    
    trieInspectFile.close()
    print("create log file:", trieInspectLogFileName)
    print("total trie inspect time:", datetime.now()-totalStartTime)



if __name__ == "__main__":
    print("start")
    startTime = datetime.now()

    # set threadpool for db querying
    pool = Pool(1)

    #
    # call APIs to simulate MPT
    #

    # set simulation options
    deleteDisk = True
    doStorageTrieUpdate = True
    stopWhenErrorOccurs = True
    doReset = True
    collectDeletedAddresses = False
    doRestore = True
    # set simulation mode (0: original Ethereum, 1: Ethane, 2: Ethanos)
    simulationMode = 0
    # set simulation params
    startBlockNum = 0
    endBlockNum = 100000
    deleteEpoch = 10000
    inactivateEpoch = 10000
    inactivateCriterion = 10000
    fromLevel = 0 # how many parent nodes to omit in Merkle proofs

    # run simulation
    if simulationMode == 0:
        # replay txs in Ethereum for Ethereum
        simulateEthereum(startBlockNum, endBlockNum)
        inspectTriesEthereum(startBlockNum, endBlockNum, inactivateCriterion)
    elif simulationMode == 1:
         # replay txs in Ethereum for Ethane
        simulateEthane(startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion, fromLevel)
        inspectTriesEthane(startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion)
        # checkEthaneStateCorrectness(endBlockNum)
    elif simulationMode == 2:
        # replay txs in Ethereum for Ethanos
        simulateEthanos(startBlockNum, endBlockNum, inactivateCriterion, fromLevel)
        inspectTriesEthanos(startBlockNum, endBlockNum, inactivateCriterion)
        # checkEthanosStateCorrectness(endBlockNum)
    else:
        print("wrong mode:", simulationMode)

    print("end")
    endTime = datetime.now()
    print("final elapsed time:", endTime-startTime)
