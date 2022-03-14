# -*- coding: utf-8 -*-

"""2021-12-31 8:48 PM (joonha)
This file is originally made to simulate storage compact Merkle-Patricia Trie (@storageCompactTrie branch),
but altered to simulate Contract Account's storage leaf node inactivation and restoration (@compactTrie_joonha branch)."""

"""(original memo) (joonha)
[storageUpdate.py 구동 순서]
1. python3 storageUpdate.py 5 수행 (coinbase를 unlock하기 위함.)
2. sampleContract를 geth 콘솔에서 수행(eth.sendTransaction ~)
3. 2번을 수행의 로그 중 contract address를 복사하여 storageUpdate.py 코드의 contractAddress 부분에 붙여넣음.
4. python3 storageUpdate.py 5 수행"""

from web3 import Web3
import sys
#import socket
import random
#import rlp
import time
#import numpy as np
import os, binascii
from datetime import datetime
from multiprocessing import Pool
from flask import Flask, request, jsonify
from flask_cors import CORS
import json


# Settings
FULL_PORT = "8083"
PASSWORD = "1234"

# Account number
# ACCOUNT_NUM = int(sys.argv[1])
TX_PER_BLOCK = 5 # 200

# multiprocessing
THREAD_COUNT = 1

# tx arguments option
INCREMENTAL_RECEIVER_ADDRESS = True # set tx receiver: incremental vs random
INCREMENTAL_SEND_AMOUNT = True      # set send amount: incremental vs same (1 wei)
MAX_ADDRESS = 0                     # set max address to set the receiver address upper bound (0 means there is no bound)

# providers
fullnode = Web3(Web3.HTTPProvider("http://localhost:" + FULL_PORT))

# Ethereum coinbase account
PASSWORD = "1234"
fullnode.geth.personal.unlockAccount(fullnode.eth.coinbase, PASSWORD, 0)
fullnode.eth.defaultAccount = fullnode.eth.coinbase

# simpleStorage2.sol smart contract
# put employ code to geth console & start mining => then we will get contract address
CONTRACTADDR = Web3.toChecksumAddress("0xe4f853b9d237b220f0ECcdf55d224c54a30032Df")
# CONTRACTADDR = Web3.toChecksumAddress("0xe66840E9821D06Ea7ca1aA8328DbA56883543f8A")
abiString = '[ { "constant": false, "inputs": [ { "name": "key", "type": "uint256" }, { "name": "value", "type": "uint256" } ], "name": "set", "outputs": [], "payable": false, "stateMutability": "nonpayable", "type": "function" }, { "constant": true, "inputs": [ { "name": "", "type": "uint256" } ], "name": "storageMap", "outputs": [ { "name": "", "type": "uint256" } ], "payable": false, "stateMutability": "view", "type": "function" }, { "constant": true, "inputs": [ { "name": "key", "type": "uint256" } ], "name": "get", "outputs": [ { "name": "", "type": "uint256" } ], "payable": false, "stateMutability": "view", "type": "function" }, { "constant": true, "inputs": [ { "name": "slot", "type": "uint256" }, { "name": "key", "type": "uint256" } ], "name": "mapLocation", "outputs": [ { "name": "", "type": "uint256" } ], "payable": false, "stateMutability": "view", "type": "function" } ]'
CONTRACTABI = json.loads(abiString)
CONTRACT = fullnode.eth.contract(address=CONTRACTADDR, abi=CONTRACTABI)

# gas amount for transaction (should be large enough)
GAS = 10000000





# dictionaries
setter = dict()
getter = dict()





def makeRandHex():
        randHex = binascii.b2a_hex(os.urandom(20))
        return Web3.toChecksumAddress("0x" + randHex.decode('utf-8'))

def intToAddr(num):
    intToHex = f'{num:0>40x}'
    return Web3.toChecksumAddress("0x" + intToHex)

# (joonha)
def getStorageAt(address, position):
    # Get the storage at a specific position of an address.
    # https://web3js.readthedocs.io/en/v1.2.11/web3-eth.html#getstorageat
    return fullnode.eth.getStorageAt(address, position, "latest")

# (joonha)
def sendContractTransaction(transactionObject):
    # Sends a transaction to the network.
    # https://web3js.readthedocs.io/en/v1.2.11/web3-eth.html#sendtransaction
    try:
        fullnode.eth.sendTransaction(transactionObject)
        print("[SUCCESS] Sending Contract Transaction succeeded.")
        return 1
    except:
        print("[FAIL] Sending Contract Transaction failed.")
        return 0

# (joonha)
def getBlockTransactionHash(blockHashOrBlockNumber):
    # Returns a block matching the block number or block hash.
    # https://web3js.readthedocs.io/en/v1.2.11/web3-eth.html#getblock
    return fullnode.eth.getBlock(blockHashOrBlockNumber).transactions # multiple Txs -> Array

# (joonha)
def contractAddress(transactionHash):
    # Returns the receipt of a transaction by transaction hash.
    # The contract address created, if the transaction was a contract creation, otherwise null.
    # https://web3js.readthedocs.io/en/v1.2.11/web3-eth.html#gettransactionreceipt
    return fullnode.eth.getTransactionReceipt(transactionHash).contractAddress






if __name__ == "__main__":

    try:
        fullnode.geth.miner.start(1)

        print("")

        # set mapping
        # CONTRACT.functions.set(88, 45).transact({'gas':GAS})
        CONTRACT.functions.set(88, 47).transact({'gas':GAS})
        # CONTRACT.functions.set(89, 33).transact({'gas':GAS})
        # CONTRACT.functions.set(88, 46).transact({'gas':GAS})
        # CONTRACT.functions.set(90, 46).transact({'gas':GAS})


        # CONTRACT.functions.set(100, 1).transact({'gas':GAS})





        # should know the slot offset to calculate the mapLocation
        # https://programtheblockchain.com/posts/2018/03/09/understanding-ethereum-smart-contract-storage/
        mapLocation = CONTRACT.functions.mapLocation(3, 88).call()
        print("Map Location:\t\t\t" + str(mapLocation))

        # get mapping
        MAP = str(CONTRACT.functions.get(88).call())
        print("Storage Map:\t\t\t" + str(MAP))

    except:
        # print("ERROR: cannot connect to Ethereum node. Start ethereum node first")
        print("WARNING: No contract account to update")
        sys.exit()




    totalStartTime = datetime.now()
    sendPool = Pool(THREAD_COUNT) # -> important: this should be in this "__main__" function

    # # transaction release
    # main()

    # storage trie of this contract
    print("\nBlock Number:\t\t\t" + str(fullnode.eth.blockNumber))
    print("Number of Transactions:\t\t" + str(len(getBlockTransactionHash(fullnode.eth.blockNumber)))) # number of Txs in this block

    updatingContractAddr = contractAddress(getBlockTransactionHash(fullnode.eth.blockNumber)[0]) # contract address of this transaction # only one contract -> [0]
    print("Updating Contract Address:\t" + str(updatingContractAddr))
    print("Updated Contract Address:\t" + str(CONTRACTADDR))

    print("")

    for i in range(mapLocation-5, mapLocation + 10): # there are (2^32 - 1) slots (=contract storage)
        print("Storage of this contract:\t" + str(i) + "\t" + str(getStorageAt(CONTRACTADDR, i).hex()))
        # print("Storage of this contract:\t" + str(i) + "\t" + str(map[i]))

    totalEndTime = datetime.now() - totalStartTime
    print("\ntotal elapsed:", totalEndTime.seconds, "seconds")
    print("DONE")