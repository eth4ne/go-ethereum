from web3 import Web3
import sys
import socket
import os
import random
# import mongoAPI
import json
import rlp
import time
import binascii
import numpy as np
from datetime import datetime

# delete epoch
EPOCH = 3

# Settings
FULL_PORT = "8083"
PASSWORD = "1234"

# providers
fullnode = Web3(Web3.HTTPProvider("http://localhost:" + FULL_PORT))

# functions
def main():

    # addr to restore
    restoreAddr = "0x0000000000000000000000000000000000000001"
    # restoreAddr = "0xe4f853b9d237b220f0ECcdf55d224c54a30032Df"
    print("joonha flag 0")

    # unlock coinbase
    fullnode.geth.personal.unlockAccount(fullnode.eth.coinbase, PASSWORD, 0)

    currentBlock = fullnode.eth.blockNumber

    # send restore tx
    print("currentBlock: ", currentBlock)
    # print("restore target block:", fullnode.eth.blockNumber)
    sendRestoreTx(fullnode.eth.blockNumber, restoreAddr)
    print("restore tx -> target:", restoreAddr)

    # start mining
    # fullnode.geth.miner.start(1)

    fullnode.geth.miner.start(1)  # start mining
    while (fullnode.eth.blockNumber == currentBlock):
        pass # just wait for mining
    fullnode.geth.miner.stop()  # stop mining

    # wait for the last block to be mined
    # fullnode.geth.miner.stop()
    print("\n")
    print(datetime.now())
    print("Block Mining Complete")

    ##################################################################################################
    ######################### below is for common.AlredyRestored check ###############################
    ##################################################################################################
    # # if below is executed, an err occurs from the Archive node
    # # because the archive node cannot find a key from the AddrToKey_inactive (when the length is 0)

    # currentBlock = fullnode.eth.blockNumber

    # # send restore tx
    # print("currentBlock: ", currentBlock)
    # # print("restore target block:", fullnode.eth.blockNumber)
    # sendRestoreTx(fullnode.eth.blockNumber, restoreAddr)
    # print("restore tx -> target:", restoreAddr)

    # # start mining
    # # fullnode.geth.miner.start(1)

    # fullnode.geth.miner.start(1)  # start mining
    # while (fullnode.eth.blockNumber == currentBlock):
    #     pass # just wait for mining
    # fullnode.geth.miner.stop()  # stop mining

    # # wait for the last block to be mined
    # # fullnode.geth.miner.stop()
    # print("\n")
    # print(datetime.now())
    # print("Block Mining Complete")

    ######################################## fin #####################################################



def sendTransaction(to, delegatedFrom):
    #print("send tx -> from:", delegatedFrom, "/ to:", to)
    while True:
        try:
            fullnode.eth.sendTransaction(
                {'to': to, 'from': fullnode.eth.coinbase, 'value': '0', 'data': delegatedFrom, 'gas': '21000'})
            break
        except:
            continue

    ##################################################################################################
    ######################### below is for common.AlredyRestored check ###############################
    ##################################################################################################

    # fullnode.geth.miner.start(1)  # start mining
    # # while (fullnode.eth.blockNumber == currentBlock):
    # #     pass # just wait for mining
    # fullnode.geth.miner.stop()  # stop mining

    # # wait for the last block to be mined
    # # fullnode.geth.miner.stop()
    # print("\n")
    # print(datetime.now())
    # print("Block Mining Complete")

    # while True:
    #     try:
    #         fullnode.eth.sendTransaction(
    #             {'to': to, 'from': fullnode.eth.coinbase, 'value': '0', 'data': delegatedFrom, 'gas': '21000'})
    #         break
    #     except:
    #         continue

    # fullnode.geth.miner.start(1)  # start mining
    # # while (fullnode.eth.blockNumber == currentBlock):
    # #     pass # just wait for mining
    # fullnode.geth.miner.stop()  # stop mining

    # # wait for the last block to be mined
    # # fullnode.geth.miner.stop()
    # print("\n")
    # print(datetime.now())
    # print("Block Mining Complete")

    ######################################## fin #####################################################



def sendRestoreTx(currentBlock, address):
    #
    latestCheckPoint = currentBlock - (currentBlock % EPOCH)
    latestCheckPoint = 0 if latestCheckPoint < 0 else latestCheckPoint

    print("restore target block:", latestCheckPoint)

    rlpeds = list()
    print("joonha flag 1")
    print("joonha flag 2")
    proofs = list()
    #
    # targetBlocks = list(range(latestCheckPoint - EPOCH, latestCheckPoint - EPOCH, -EPOCH))
    targetBlocks = list(range(latestCheckPoint, latestCheckPoint, -EPOCH))
    #
    targetBlocks.append(latestCheckPoint)
    print(" restore target address:", address, "/ target blocks:", targetBlocks)
    for targetBlock in targetBlocks:
        proof = fullnode.eth.getProof(
            Web3.toChecksumAddress(address),
            [],
            block_identifier=targetBlock
        )
        print(" target block num:", targetBlock, "/ raw proof:", proof)
        proofs.append(proof)
        # if proof['restored']:
        #     break

    #print(currentBlock, proofs, targetBlocks)

    # if break when restored = true -> cut out targetBlocks too
    if len(proofs) != len(targetBlocks):
        print("set targetblocks correctly when restored = true")
        targetBlocks = targetBlocks[:len(proofs)]

    print("proofs: ", proofs)

    proofs.reverse()
    print("reverse proofs: ", proofs)
    targetBlocks.reverse()

    print("\n\ntargetBlocks: ", targetBlocks, "\n\n")

    # print("\ntarget block:", currentBlock, "/ target address:", address)
    print(" print proof before to be compact")
    # for i in range(len(proofs)):
    #     print(" at block:", targetBlocks[i] ,"/ isExist:", not proofs[i]['IsVoid'], "/ isBloom:", proofs[i]['isBloom'])

    """
    Compact Form Proof
    """
    # tmps = proofs[:]  # deep copy
    # for tmp in tmps:
    #     proofs.pop(0)
    #     targetBlocks.pop(0)

        # if tmp['IsVoid']:
        #     proofs.pop(0)
        #     targetBlocks.pop(0)
        # else:
        #     break

    # tmps = proofs[:]
    # for i, tmp in enumerate(tmps):
    #     try:
    #         proofs.pop(0)
    #         targetBlocks.pop(0)
    #     except:
    #         pass
    #     # try:
    #     #     if (tmps[i + 1])['IsVoid']:
    #     #         break
    #     #     else:
    #     #         proofs.pop(0)
    #     #         targetBlocks.pop(0)
    #     # except:
    #     #     pass

    preRlp = list()
    preRlp.append(address)
    preRlp.append(0 if len(targetBlocks) == 0 else targetBlocks[0])
    if len(targetBlocks) == 0: # this can happen -> just send target address & 0
        print(" no target block")
        pass

    # isBlooms = list()
    for proof in proofs:
        pfs = proof['accountProof']
        preRlp.append(len(pfs))
        for pf in pfs:
            preRlp.append(pf)

    # print("> preRlp: ", preRlp)

    print("preRlp: ", preRlp)

    rlped = rlp.encode(preRlp)
    # print("> rlped : ", rlped)
    rlpeds.append(len(binascii.hexlify(rlped)))

    # print(" print compact proof")
    # for i in range(len(proofs)):
    #     print(" at block:", targetBlocks[i] ,"/ isExist:", not proofs[i]['IsVoid'], "/ isBloom:", proofs[i]['isBloom'])

    print("rlped: ", rlped)
    sendTransaction("0x0123456789012345678901234567890123456789", rlped)
    # print("Restore Tx# {0}".format(r), end="\r")

    return min(rlpeds), max(rlpeds), np.average(rlpeds)







if __name__ == "__main__":
    main()
    print("DONE")