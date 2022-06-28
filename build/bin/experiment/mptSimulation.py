from web3 import Web3
import socket
import os, binascii
import sys
from datetime import datetime

# simulator server IP address
SERVER_IP = "localhost"
SERVER_PORT = 8999

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
def inspectCurrentTrie():
    cmd = str("inspectCurrentTrie")
    client_socket.send(cmd.encode())
    data = client_socket.recv(1024)
    inspectResult = data.decode().split(',')
    currentTrieNodesNum = int(inspectResult[0])
    currentTrieNodesSize = int(inspectResult[1])
    print("inspectCurrentTrie result -> # of current trie nodes:", '{:,}'.format(currentTrieNodesNum), "/ current trie size:", '{:,}'.format(currentTrieNodesSize), "B")
    return inspectResult

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
    print("response:", data.decode())

# generate random address
def makeRandAddr():
    randHex = binascii.b2a_hex(os.urandom(20))
    # print("randHex:", randHex, "/ len:", len(randHex), "/ type:", type(randHex))
    return Web3.toChecksumAddress("0x" + randHex.decode('utf-8'))

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
            print("flush! inserted", i+1, "accounts")
            flush()
    flush()
    endTime = datetime.now()
    print("\nupdate", accNumToInsert, "accounts / flush epoch:", flushEpoch, "/ elapsed time:", endTime - startTime)
    inspectCurrentTrie()
    inspectDB()

if __name__ == "__main__":
    print("start")

    #
    # call APIs to simulate MPT
    #

    # for test, run this function
    generateSampleTrie()
    # sys.exit()

    print("end")
