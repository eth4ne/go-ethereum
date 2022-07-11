from web3 import Web3
import socket
import os, binascii
import sys
from datetime import datetime

# simulator server IP address
SERVER_IP = "localhost"
SERVER_PORT = 8999

# maximum byte length of response from the simulator
maxResponseLen = 4096

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
# inspectResult: total nodes num/size, fullNode num/size, shortNode num/size, leafNode num/size
def inspectTrie():
    cmd = str("inspectTrie")
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    inspectResult = data.decode().split(',')
    print("inspectTrie result ->", inspectResult)
    return inspectResult

# get number and size of trie nodes in certain state sub trie
# inspectResult: total nodes num/size, fullNode num/size, shortNode num/size, leafNode num/size
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

# update trie (key: keyString, value: account)
def updateTrie(keyString):
    cmd = str("updateTrie")
    cmd += str(",")
    cmd += keyString
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
        # updateTrie(makeRandHashLengthHexString()) # insert random keys
        updateTrie(intToHashLengthHexString(i)) # insert incremental keys (Ethane)
        if (i+1) % flushEpoch == 0:
            print("flush! inserted", '{:,}'.format(i+1), "accounts")
            flush()
    flush()
    endTime = datetime.now()
    print("\nupdate", '{:,}'.format(accNumToInsert), "accounts / flush epoch:", flushEpoch, "/ elapsed time:", endTime - startTime)
    inspectTrie()
    inspectDB()


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
        updateTrie(key)
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





if __name__ == "__main__":
    print("start")

    #
    # call APIs to simulate MPT
    #

    # for test, run this function
    generateSampleTrie()
    # sys.exit()

    print("end")
