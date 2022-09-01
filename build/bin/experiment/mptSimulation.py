from web3 import Web3
import socket
import os, binascii
import sys
from datetime import datetime
import pymysql.cursors

# ethereum tx data db options
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

# open socket
client_socket = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
# connect to the server
client_socket.connect((SERVER_IP, SERVER_PORT))

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
def select_slot_read_write_list(cursor, stateid):
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
    accNumToInsert = 100
    flushEpoch = 4
    for i in range(accNumToInsert):
        # updateTrieSimple(makeRandHashLengthHexString()) # insert random keys
        updateTrieSimple(intToHashLengthHexString(i)) # insert incremental keys (Ethane)
        if (i+1) % flushEpoch == 0:
            print("flush! inserted", '{:,}'.format(i+1), "accounts")
            flush()
    flush()
    endTime = datetime.now()
    print("\nupdate", '{:,}'.format(accNumToInsert), "accounts / flush epoch:", flushEpoch, "/ elapsed time:", endTime - startTime)
    inspectTrie()
    inspectDB()


# for heuristic strategy, find shortest prefixes among short nodes in the current trie
def findShortestPrefixAmongShortNodes(maxPrefixesNum):
    cmd = str("findShortestPrefixAmongShortNodes")
    cmd += str(",")
    cmd += str(maxPrefixesNum)
    client_socket.send(cmd.encode())
    data = client_socket.recv(maxResponseLen)
    findResult = data.decode().split(',')
    # print("findResult:", findResult)
    shortestPrefixes = findResult[:-1] # ignore "success" field
    # print("shortest prefixes:", shortestPrefixes, "/ success:", findResult[-1])
    return shortestPrefixes

def findAddrHashWithPrefix(prefixes):
    if prefixes[0] == "":
        return makeRandAddrHash()
    else:
        prefixLen = len(prefixes[0])
        count = 0
        maxTry = 1000
        while True:
            count += 1
            addr, addrHash = makeRandAddrHash()
            # print("addrHash[:prefixLen]:", addrHash[:prefixLen])
            # print("prefix:", prefix)
            if addrHash[:prefixLen] in prefixes:
                # print("matching prefix:", addrHash[:prefixLen], "/ addrHash:", addrHash)
                return addr, addrHash
            if count > maxTry:
                # print("cannot find proper address, just return random addrHash")
                return makeRandAddrHash()

# heuristic 1
def strategy_heuristic_splitShortNodes(flushEpoch, attackNumPerBlock, accNumToInsert, doRealComputation=False):
    # initialize
    reset()
    updateCount = 0
    randomInsertNum = 0
    heuristicInsertNum = 0
    prefixLenSum = 0
    startTime = datetime.now()

    # insert random accounts, then insert heuristically seleted accounts
    if attackNumPerBlock > flushEpoch:
        print("attack num per block cannot be larger than flush epoch, just return")
        return
    
    while updateCount < accNumToInsert:
        # insert random accounts
        for i in range(flushEpoch-attackNumPerBlock):
            updateTrieSimple(makeRandHashLengthHexString())
        randomInsertNum += flushEpoch-attackNumPerBlock
        
        # insert heuristic accounts
        attackNum = 0
        while attackNum < attackNumPerBlock:
            shortestPrefixes = findShortestPrefixAmongShortNodes(attackNumPerBlock)
            possibleAttackNum = len(shortestPrefixes)
            if possibleAttackNum > attackNumPerBlock - attackNum:
                possibleAttackNum = attackNumPerBlock - attackNum
            for i in range(possibleAttackNum):
                prefix = shortestPrefixes[i]
                prefixLenSum += len(prefix)
                if doRealComputation:
                    # real computation: find proper address whose hash value has certain prefix
                    addr, addrHash = findAddrHashWithPrefix(prefix)
                else:
                    # sudo computation: just generate random string with certain prefix
                    addrHash = prefix + makeRandHashLengthHexString()[len(prefix):]
                # print("prefixes:", shortestPrefixes, "/ addrHash:", addrHash)
                updateTrieSimple(addrHash)
            heuristicInsertNum += possibleAttackNum
            attackNum += possibleAttackNum

        # insert random accounts (when there is not enough attack space)
        # for i in range(attackNumPerBlock - possibleAttackNum):
        #     updateTrieSimple(makeRandHashLengthHexString())
        # randomInsertNum += attackNumPerBlock - possibleAttackNum
        
        # flush
        updateCount += flushEpoch
        print("flush! inserted", '{:,}'.format(updateCount), "accounts / elapsed time:", datetime.now()-startTime)
        flush()

    # final flush
    flush()

    # show final result
    print("strategy_heuristic_splitShortNodes() finished")
    print("total elapsed time:", datetime.now()-startTime)
    print("total inserts:", accNumToInsert, "-> random inserts:", randomInsertNum, " / heuristic inserts:", heuristicInsertNum)
    avgPrefixLen = round(prefixLenSum/heuristicInsertNum, 3)
    print("average prefix len:", avgPrefixLen)
    inspectTrie()
    inspectDB()

    # save result as a file
    logFileName = "strategy_heuristic_splitShortNodes_" + str(flushEpoch) + "_" + str(attackNumPerBlock) + \
                    "_" + str(accNumToInsert) + "_" + str(heuristicInsertNum) + "_" + str(avgPrefixLen) + ".txt"
    printAllStats(logFileName)
    print("create log file:", logFileName)
    # printCurrentTrie()
    # trieToGraph()





# heuristic 2: fill all full node's child hashes
def strategy_heuristic_fillFullNodes():
    # initialize
    reset()
    updateCount = 0
    startTime = datetime.now()
    
    # set options
    flushEpoch = 400
    doRealComputation = False
    totalAccNumToInsert = 1000000
    maxPrefixLen = 1
    logFileName = "strategy_heuristic_fillFullNodes" + "_" + str(maxPrefixLen) + "_" + str(flushEpoch) + "_" + str(totalAccNumToInsert) + ".txt"

    # insert accounts to make fullfilled full nodes (i.e., have 16 children)
    heuristicInsertNum = int(min(16**maxPrefixLen, totalAccNumToInsert))
    print("heuristicInsertNum:", heuristicInsertNum)    
    for i in range(heuristicInsertNum):
        prefix = hex(i)[2:] # delete 0x
        prefix = prefix.zfill(maxPrefixLen) # zero padding
        # print("prefix:", prefix)
        if doRealComputation:
            # real computation
            shortestPrefixes = prefix
            addr, addrHash = findAddrHashWithPrefix(shortestPrefixes)
        else:
            # sudo computation
            addrHash = prefix + makeRandHashLengthHexString()[len(prefix):]
        # print("prefix:", prefix, "/ addrHash:", addrHash)

        updateTrieSimple(addrHash)
        updateCount += 1
        if updateCount % flushEpoch == 0:
            print("flush! inserted", '{:,}'.format(i+1), "accounts / elapsed time:", datetime.now()-startTime)
            flush()
    
    # show intermediate result
    inspectTrie()
    inspectDB()

    # insert random accounts
    randomInsertNum = totalAccNumToInsert - heuristicInsertNum
    print("randomInsertNum:", randomInsertNum)
    for i in range(randomInsertNum):
        updateTrieSimple(makeRandHashLengthHexString())
        updateCount += 1
        if updateCount % flushEpoch == 0:
            print("flush! inserted", '{:,}'.format(i+1), "accounts / elapsed time:", datetime.now()-startTime)
            flush()
    flush()

    # show final result
    print("strategy_heuristic_fillFullNodes() finished")
    print("total elapsed time:", datetime.now()-startTime)
    print("max prefix len:", maxPrefixLen)
    print("heuristic inserts:", heuristicInsertNum, "/ random inserts:", randomInsertNum)
    # printCurrentTrie()
    inspectTrie()
    inspectDB()
    printAllStats(logFileName)





# just insert random keys
def strategy_random(flushEpoch, totalAccNumToInsert):
    # initialize
    reset()
    updateCount = 0
    startTime = datetime.now()

    # set log file name
    logFileName = "strategy_random_" + str(flushEpoch) + "_" + str(totalAccNumToInsert) + ".txt"

    # insert random accounts
    for i in range(totalAccNumToInsert):
        key = makeRandHashLengthHexString()
        updateTrieSimple(key)
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





# replay txs in Ethereum with original Ethereum client
def simulateEthereum(startBlockNum, endBlockNum):

    switchSimulationMode(0) # 0: original geth mode

    # initialize
    reset()
    updateCount = 0
    readCount = 0
    writeCount = 0
    startTime = datetime.now()

    # set log file name
    logFileName = "ethereum_simulate_" + str(startBlockNum) + "_" + str(endBlockNum) + ".txt"

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

                print("in block", blockNum, ", find write item", writeCount)
                # print("write account ->")
                # print("  addr:", addr)
                # print("  nonce:", nonce)
                # print("  balance:", balance)
                # print("  codeHash:", codeHash)
                # print("  storageRoot:", storageRoot)
                # print("\n")
                updateTrie(nonce, balance, storageRoot, codeHash, addr)
            else:
                readCount += 1

        # printCurrentTrie()
        # flush
        flush()
        print("flush finished -> current block num:", blockNum)

    # show final result
    print("simulateEthereum() finished -> from block", startBlockNum, "to", endBlockNum)
    print("total elapsed time:", datetime.now()-startTime)
    print("total writes:", writeCount, "/ total reads:", readCount)
    # inspectTrie()
    # printCurrentTrie()
    inspectDB()
    printAllStats(logFileName)
    print("create log file:", logFileName)





if __name__ == "__main__":
    print("start")
    switchSimulationMode(0) # 0: original geth mode

    #
    # call APIs to simulate MPT
    #

    # ex1. strategy: random
    # strategy_random(400, 100000)
    # ex2. strategy: split short nodes
    # strategy_heuristic_splitShortNodes(400, 40, 100000, False)

    print("end")
