from web3 import Web3
import socket
import os, binascii
import sys
import random
from datetime import datetime

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





if __name__ == "__main__":
    print("start")
    switchSimulationMode(1) # 1: Ethane

    #
    # call APIs to simulate MPT
    #
    
    # ex1. strategy: random
    strategy_random(100, 1000, 10)
    printEthaneState()

    print("end")
