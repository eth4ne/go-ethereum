from typing import (
  Callable,
  NewType,
  Union,
)
from web3 import Web3, module
from web3.method import (
  Method,
  default_root_munger,
)
from web3.types import (
  BlockNumber,
  RPCEndpoint,
)

import pymysql.cursors
import time
import threading
import json
import rlp
import binascii

db_host = 'localhost'
db_user = 'ethereum'
db_pass = '1234' #fill in the MariaDB/MySQL password.
db_name = 'ethereum'

# geth_ipc = './bin/data/geth.ipc' #fill in the IPC path.
geth_ipc = '/ethereum/geth-test-joonha/geth.ipc' #fill in the IPC path.

num_block = 1 # number of blocks to make at once
epoch = 3 # inactivate epoch
restore_offset = 0
password = '1234' #fill in the geth coinbase password.

EthToWei = 1000000000000000000

MODE_ETHANOS = 0
MODE_ETHANE = 1
execution_mode = MODE_ETHANE

RESTORE_ALL = 0
RESTORE_RECENT = 1
RESTORE_OLDEST = 2
RESTORE_OPTIMIZED = 3 # If the amount is given as an input, then automatically restore the least accounts whose sum is bigger than the amount
restore_amount = '50' # requesting amount to be restored (only RESTORE_OPTIMIZED mode)
restore_mode = RESTORE_ALL

restorefile = 'restore_test.json'

NUM_NORMAL_TX = 5
OFFSET_ADDR = 1

conn_geth = lambda path: Web3(Web3.IPCProvider(path))
conn_mariadb = lambda host, user, password, database: pymysql.connect(host=host, user=user, password=password, database=database, cursorclass=pymysql.cursors.DictCursor)

class Debug(module.Module):
  def __init__(self, web3):
    module.Module.__init__(self, web3)
  
  setHead: Method[Callable[[BlockNumber], bool]] = Method(
    RPCEndpoint("debug_setHead"),
    mungers=[default_root_munger],
  )

Nonce = NewType("Nonce", int)
Wei = NewType('Wei', int)

class Custom(module.Module):
  def __init__(self, web3):
    module.Module.__init__(self, web3)

class Worker(threading.Thread):
  def __init__(self, web3, tx):
    threading.Thread.__init__(self)
    self.web3 = web3
    self.tx = tx
  
  def run(self):
    self.web3.eth.send_transaction(self.tx)
  

def run(num):
  web3 = conn_geth(geth_ipc)
  conn = conn_mariadb(db_host, db_user, db_pass, db_name)

  web3.attach_modules({
    'debug': Debug,
    'custom': Custom,
  })

  #web3.debug.setHead('0x0')

  coinbase = web3.eth.coinbase
  web3.geth.personal.unlock_account(coinbase, password, 0)

  # stop mining
  web3.geth.miner.stop()

  offset_block = web3.eth.get_block_number() + 1 # _from # - 1
  realblock = 0

  print('Account {} unlocked'.format(coinbase))
  print('Run from {} to {}'.format(offset_block, offset_block+num-1))

  with open(restorefile, 'r') as f:
    restoredata = f.read()
    restoredata = json.loads(restoredata)

  for i in range(num):
    currentBlock = web3.eth.get_block_number()
    print("currentBlock: ", currentBlock)
    workers = []

    """ RESTORE TX """
    if str(offset_block+i-restore_offset) in restoredata:
      for j in restoredata[str(offset_block+i-restore_offset)]:
        # j는 address가 되어야 함.
        tx = makeRestoreTx(web3, offset_block+i-restore_offset, j, 100) # gasPrice is 100
        if tx is None:
          print("(No Txs to Restore)")
          continue
        worker = Worker(web3, tx)
        worker.start()
        workers.append(worker)
    
      print('Block #{}: Restored {} accounts'.format(offset_block+i, len(restoredata[str(offset_block+i-restore_offset)])))

    """ NORMAL TX """
    for j in range(NUM_NORMAL_TX): # for each tx in one block
      to = intToAddr(int(OFFSET_ADDR+j))
      amount = int(OFFSET_ADDR+j)
      tx = {
        'from': coinbase,
        'to': to,
        'value': amount,
        'gas': '21000',
        # 'nonce': hex(int(i*NUM_NORMAL_TX+j)),
        'data': '',
      }

      print("NORMAL TX")
      print(tx)

      worker = Worker(web3, tx)
      worker.start()
      workers.append(worker)
    
    """ MINING """
    for j in workers:
      j.join()

    print('Block #{}: processed all txs'.format(i))
    
    web3.geth.miner.start(1)
    while (web3.eth.get_block_number() == currentBlock):
      # time.sleep(0.001)
      pass # just wait for mining
    web3.geth.miner.stop()

    realblock = web3.eth.get_block_number()
    print('Mined block #{}'.format(realblock))

    print('='*60)

def intToAddr(num):
    intToHex = f'{num:0>40x}'
    return Web3.toChecksumAddress("0x" + intToHex)

def makeRestoreTx(web3, currentBlock, address, gasprice):
  print('Restore: {} at {}'.format(address, currentBlock))
  latestCheckPoint = currentBlock - ((currentBlock+1) % epoch)
  latestCheckPoint = 0 if latestCheckPoint < 0 else latestCheckPoint

  rlpeds = list()
  proofs = list()
  
  targetBlocks = list(range(latestCheckPoint, latestCheckPoint, -epoch))
  
  targetBlocks.append(latestCheckPoint)
  
  if restore_mode == RESTORE_ALL:
    rmode = ['00']
  elif restore_mode == RESTORE_RECENT:
    rmode = ['01']
  elif restore_mode == RESTORE_OLDEST:
    rmode = ['02']
  elif restore_mode == RESTORE_OPTIMIZED:
    rmode = ['03', restore_amount]
  
  for targetBlock in targetBlocks:
    try:
      proof = web3.eth.getProof(
        Web3.toChecksumAddress(address),
        rmode, # specify the restore option here (joonha)
        block_identifier=targetBlock
      )
    except: # do not stop even if there is no account to restore (joonha)
      print("nil proof")
      return

    proofs.append(proof)

  if len(proofs) != len(targetBlocks):
    targetBlocks = targetBlocks[:len(proofs)]

  proofs.reverse()
  targetBlocks.reverse()

  preRlp = list()
  preRlp.append(address)
  preRlp.append(0 if len(targetBlocks) == 0 else targetBlocks[0])

  for proof in proofs:
    pfs = proof['accountProof']
    preRlp.append(len(pfs))
    for pf in pfs:
      preRlp.append(pf)

  rlped = rlp.encode(preRlp)

  data = rlped.hex()

  tx = {
    'from': web3.eth.coinbase,
    'to': "0x0123456789012345678901234567890123456789",
    'value': '0x0',
    'data': data,
    'gas': '0x0',
  }

  print("RESTORE TX")
  print(tx)

  return tx

run(num_block)