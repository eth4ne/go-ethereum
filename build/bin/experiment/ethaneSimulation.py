from web3 import Web3
import socket
import os, binascii
import sys
import random
from datetime import datetime
import pymysql.cursors

# ethereum tx data DB options
db_host = 'localhost'
db_user = 'ethereum'
db_pass = '1234' # fill in the MariaDB/MySQL password.
db_name = 'ethereum' # block 0 ~ 1,000,000
conn_mariadb = lambda host, user, password, database: pymysql.connect(host=host, user=user, password=password, database=database, cursorclass=pymysql.cursors.DictCursor)
conn = conn_mariadb(db_host, db_user, db_pass, db_name)
cursor = conn.cursor()

# simulator server IP address
SERVER_IP = "localhost"
SERVER_PORT = 8999

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

# read block header
def select_block_header(cursor, blocknumber):
    sql = "SELECT * FROM blocks WHERE `number`=%s;"
    cursor.execute(sql, (blocknumber,))
    result = cursor.fetchall()
    return result[0]

# read tx hash of block [blocknumber]'s [txindex]^th tx
def select_tx_hash(cursor, blocknumber, txindex):
    sql = "SELECT * FROM transactions WHERE `blocknumber`=%s AND `transactionindex`=%s;"
    cursor.execute(sql, (blocknumber, txindex))
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

# read storage trie slots r/w list in this block from DB
def select_slot_write_list(cursor, stateid):
    sql = "SELECT * FROM `slotlogs` WHERE `stateid`=%s;"
    cursor.execute(sql, (stateid,))
    result = cursor.fetchall()
    return result

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
    print("root hash:", rootHash)
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
def inspectDB():
    cmd = str("inspectDB")
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    inspectResult = data.decode().split(',')
    totalTrieNodesNum = int(inspectResult[0])
    TotalTrieNodesSize = int(inspectResult[1])
    print("inspectDB result -> # of total trie nodes:", '{:,}'.format(totalTrieNodesNum), "/ total db size:", '{:,}'.format(TotalTrieNodesSize), "B")
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
    print("flush result -> # of new trie nodes:", '{:,}'.format(newTrieNodesNum), "/ increased db size:", '{:,}'.format(newTrieNodesSize), "B")
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

# reset simulator
def reset():
    cmd = str("reset")
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

# select simulation mode (0: Geth, 1: Ethane)
def switchSimulationMode(mode):
    cmd = str("switchSimulationMode")
    cmd += str(",")
    cmd += str(mode)
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    switchSimulationModeResult = data.decode()
    print("switchSimulationModeResult result:", switchSimulationModeResult)
    return switchSimulationModeResult

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
    inspectDB()

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
def setEthaneOptions(deleteEpoch, inactivateEpoch, inactivateCriterion):
    cmd = str("setEthaneOptions")
    cmd += str(",")
    cmd += str(deleteEpoch)
    cmd += str(",")
    cmd += str(inactivateEpoch)
    cmd += str(",")
    cmd += str(inactivateCriterion)
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    setOptionResult = data.decode()
    # print("setOptionResult result:", setOptionResult)
    return setOptionResult

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
    inspectDB()
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
    inspectDB()
    printCurrentTrie()

# replay txs in Ethereum with original Ethereum client
def simulateEthereum(startBlockNum, endBlockNum):
    # set Ethereum's options
    switchSimulationMode(0) # 0: original geth mode

    # initialize
    reset()
    updateCount = 0
    readCount = 0
    writeCount = 0
    startTime = datetime.now()

    # set log file name
    logFileName = "ethereum_simulate_" + str(startBlockNum) + "_" + str(endBlockNum) + ".txt"

    currentStorageRoot = ""
    # insert random accounts
    for blockNum in range(startBlockNum, endBlockNum+1):
        print("do block", blockNum ,"\n")
        # get read/write list from DB
        rwList = select_account_read_write_list(cursor, blockNum)
        
        # replay writes
        for item in rwList:
            # print("item:", item)
            if item['type'] % 2 == 1:
                # print("item:", item)
                writeCount += 1

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

                # print("in block", blockNum, ", find write item", writeCount)
                # print("write account ->")
                # print("  addr:", addr)
                # print("  nonce:", nonce)
                # print("  balance:", balance)
                # print("  codeHash:", codeHash)
                # print("  storageRoot:", storageRoot)
                # print("\n")

                # update storage trie
                slotWriteList = select_slot_write_list(cursor, item['id'])
                if len(slotWriteList) != 0:
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
                        currentStorageRoot = updateStorageTrie(contractAddress, slot, slotValue)

                    # check current storage trie is made correctly
                    if currentStorageRoot[2:] != storageRoot:
                        print("blocknum:", blockNum)
                        # print("txindex:", item['txindex'])
                        # print("tx hash:", select_tx_hash(cursor, blockNum, item['txindex']))
                        print("item:", item)
                        print("  addr:", addr)
                        print("  nonce:", nonce)
                        print("  balance:", balance)
                        print("  codeHash:", codeHash)
                        print("  storageRoot:", storageRoot)
                        print(currentStorageRoot[2:], "vs", storageRoot)
                        sys.exit()
                
                # update state trie
                updateTrie(nonce, balance, storageRoot, codeHash, addr)
            else:
                readCount += 1

        # printCurrentTrie()
        # flush
        flush()
        print("flush finished -> current block num:", blockNum)

        # check current trie is made correctly
        stateRootInBlockHeader = select_block_header(cursor, blockNum)['stateroot'].hex()
        currentStateRoot = getTrieRootHash()[2:]
        if currentStateRoot != stateRootInBlockHeader:
            print("@@fail")
            print(currentStateRoot, "vs", stateRootInBlockHeader)
            sys.exit()
        else:
            print("@@success")
            print(currentStateRoot, "vs", stateRootInBlockHeader)

    # show final result
    print("simulateEthereum() finished -> from block", startBlockNum, "to", endBlockNum)
    print("total elapsed time:", datetime.now()-startTime)
    print("total writes:", writeCount, "/ total reads:", readCount)
    # inspectTrie()
    # printCurrentTrie()
    inspectTrieWithinRange()
    inspectDB()
    printAllStats(logFileName)
    print("create log file:", logFileName)

# replay txs in Ethereum with Ethane client
def simulateEthane(startBlockNum, endBlockNum, deleteEpoch, inactivateEpoch, inactivateCriterion):
    # set Ethane's options
    switchSimulationMode(1) # 1: Ethane mode
    setEthaneOptions(deleteEpoch, inactivateEpoch, inactivateCriterion)

    # initialize
    reset()
    updateCount = 0
    readCount = 0
    writeCount = 0
    startTime = datetime.now()

    # set log file name
    logFileName = "ethane_simulate_" + str(startBlockNum) + "_" + str(endBlockNum) \
        + "_" + str(deleteEpoch) + "_" + str(inactivateEpoch) + "_" + str(inactivateCriterion) + ".txt"

    # insert random accounts
    for blockNum in range(startBlockNum, endBlockNum+1):
        # get read/write list from DB
        rwList = select_account_read_write_list(cursor, blockNum)
        
        # replay writes
        for item in rwList:
            # print("item:", item)
            if item['type'] % 2 == 1:
                writeCount += 1

                addr = item['address'].hex()
                nonce = item['nonce']
                balance = item['balance']
                codeHash = item['codehash'].hex()
                storageRoot = item['storageroot'].hex()

                # print("in block", blockNum, ", find write item", writeCount)
                # print("write account ->")
                # print("  addr:", addr)
                # print("  nonce:", nonce)
                # print("  balance:", balance)
                # print("  codeHash:", codeHash)
                # print("  storageRoot:", storageRoot)
                # print("\n")
                updateTrieForEthane(nonce, balance, storageRoot, codeHash, addr)
            else:
                readCount += 1

        # printCurrentTrie()
        # flush
        flush()
        print("flush finished -> current block num:", blockNum)

    # show final result
    print("simulateEthane() finished -> from block", startBlockNum, "to", endBlockNum)
    print("total elapsed time:", datetime.now()-startTime)
    print("total writes:", writeCount, "/ total reads:", readCount)
    # inspectTrie()
    # printCurrentTrie()
    printEthaneState()
    inspectDB()
    printAllStats(logFileName)
    print("create log file:", logFileName)



if __name__ == "__main__":
    print("start")
    

    #
    # call APIs to simulate MPT
    #
    
    # ex1. strategy: random
    # strategy_random(100, 1000, 10)
    # printEthaneState()

    # ex2. replay txs in Ethereum for Ethereum
    simulateEthereum(1, 10000)

    # ex3. replay txs in Ethereum for Ethane
    # simulateEthane(1, 1000000, 40320, 40320, 40320)

    print("end")
