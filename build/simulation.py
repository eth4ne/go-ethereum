from typing import (
  Callable,
  NewType,
  Union,
)
from eth_typing import (
  Address,
  ChecksumAddress,
  HexStr,
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
from web3._utils.compat import (
    TypedDict,
)
from hexbytes import (
    HexBytes,
)

import pymysql.cursors
import time
import threading
import json
import rlp
import binascii

db_host = 'localhost'
db_user = 'ethereum'
db_pass = '' #fill in the MariaDB/MySQL password.
db_name = 'ethereum'

geth_ipc = '' #fill in the IPC path.

start_block = 7000001
end_block = 7300000
password = '' #fill in the geth coinbase password.
epoch = 315
restore_offset = -1
restore_offset = 0
EthToWei = 1000000000000000000

MODE_ETHANOS = 0
MODE_ETHANE = 1
execution_mode = MODE_ETHANOS

restorefile = 'restore_315_7000001_7010000.json'

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
DelegatedTxParams = TypedDict("DelegatedTxParams", {
    "chainId": int,
    "data": Union[bytes, HexStr],
    # addr or ens
    "from": Union[Address, ChecksumAddress, str],
    "gas": int,
    # legacy pricing
    "gasPrice": Wei,
    # dynamic fee pricing
    "maxFeePerGas": Union[str, Wei],
    "maxPriorityFeePerGas": Union[str, Wei],
    "nonce": Nonce,
    # addr or ens
    "to": Union[Address, ChecksumAddress, str],
    "type": Union[int, HexStr],
    "value": Wei,
    "delegatedFrom": Union[Address, ChecksumAddress, str],
}, total=False)

class Custom(module.Module):
  def __init__(self, web3):
    module.Module.__init__(self, web3)
  
  setAllowZeroTxBlock: Method[Callable[[bool], None]] = Method(
    RPCEndpoint("eth_setAllowZeroTxBlock"),
    mungers=[default_root_munger],
  )

  getAllowZeroTxBlock: Method[Callable[[bool], None]] = Method(
    RPCEndpoint("eth_getAllowZeroTxBlock"),
    mungers=[default_root_munger],
  )

  setAllowConsecutiveZeroTxBlock: Method[Callable[[bool], None]] = Method(
    RPCEndpoint("eth_setAllowConsecutiveZeroTxBlock"),
    mungers=[default_root_munger],
  )

  getAllowConsecutiveZeroTxBlock: Method[Callable[[bool], None]] = Method(
    RPCEndpoint("eth_getAllowConsecutiveZeroTxBlock"),
    mungers=[default_root_munger],
  )

  _send_delegated_transaction: Method[Callable[[DelegatedTxParams], HexBytes]] = Method(
    RPCEndpoint("eth_sendDelegatedTransaction"),
    mungers=[default_root_munger],
  )

  def send_delegated_transaction(self, transaction: DelegatedTxParams) -> HexBytes:
    return self._send_delegated_transaction(transaction)

  

class Worker(threading.Thread):
  def __init__(self, web3, tx):
    threading.Thread.__init__(self)
    self.web3 = web3
    self.tx = tx
  
  def run(self):
    self.web3.custom.send_delegated_transaction(self.tx)

def get_tx_pool_len(web3):
  txpool = web3.geth.txpool.inspect()
  pending = sum([len(v) for k, v in txpool['pending'].items()])
  queued = sum([len(v) for k, v in txpool['queued'].items()])
  return pending + queued
  

class RestoreWorker(threading.Thread):
  def __init__(self, web3, tx):
    threading.Thread.__init__(self)
    self.web3 = web3
    self.tx = tx
  
  def run(self):
    self.web3.eth.sendTransaction(self.tx)
  

def run(_from, _to):
  totalblock = 0
  totaltx = 0
  start = time.time()

  web3 = conn_geth(geth_ipc)
  conn = conn_mariadb(db_host, db_user, db_pass, db_name)
  db = conn.cursor()

  web3.attach_modules({
    'debug': Debug,
    'custom': Custom,
  })

  #web3.debug.setHead('0x0')

  coinbase = web3.eth.coinbase
  web3.geth.personal.unlock_account(coinbase, password, 0)

  print('Account {} unlocked'.format(coinbase))
  print('Run from {} to {}'.format(_from, _to))

  offset = _from # - 1
  realblock = 0
  txcount = 0
  
  gasprice_init = 1000000000

  with open(restorefile, 'r') as f:
    restoredata = f.read()
    restoredata = json.loads(restoredata)

  web3.custom.setAllowZeroTxBlock(False)
  web3.custom.setAllowConsecutiveZeroTxBlock(False)

  for i in range(_from, _to+1):
    db.execute("SELECT * from `transactions` WHERE `blocknumber`=%s;", (i,))
    result = db.fetchall()
    print('Block #{}: fetched {} txs'.format(i, len(result)))
    workers = []

    if str(i-restore_offset) in restoredata:
      for j in restoredata[str(i-restore_offset)]:
        txcount += 1
        tx = makeRestoreTx(web3, i-restore_offset - offset, j, gasprice_init-txcount)
        worker = RestoreWorker(web3, tx)
        worker.start()
        workers.append(worker)
    
      print('Block #{}: Restored {} accounts'.format(i, len(restoredata[str(i-restore_offset)])))

    for j in result:
      to = j['to']
      txcount += 1
      if to != None:
        to = Web3.toChecksumAddress(to.hex())
      tx = {
        'from': coinbase,
        'to': to,
        'value': hex(int(j['value'])),
        'delegatedFrom': Web3.toChecksumAddress(j['from'].hex()),
        'gas': '0x0',
        'gasPrice': hex(gasprice_init-txcount),
      }
      if execution_mode == MODE_ETHANE:
        tx['data'] = '0x'+j['input'].hex()
      elif execution_mode == MODE_ETHANOS:
        if to == None:
          db.execute("SELECT * from `contracts` WHERE `creationtx`=%s;", (j['hash'],))
          result = db.fetchall()
          tx['to'] = Web3.toChecksumAddress(result[0]['address'].hex())
      worker = Worker(web3, tx)
      worker.start()
      workers.append(worker)
    if len(result) == 0:
      web3.custom.setAllowZeroTxBlock(True)
      time.sleep(0.01)
    else:
      web3.custom.setAllowZeroTxBlock(False)
      time.sleep(0.01)
      
    totaltx += len(result)

    db.execute("SELECT * from `blocks` WHERE `number`=%s;", (i,))
    blocks = db.fetchone()
    miner = Web3.toChecksumAddress(blocks['miner'].hex())
    txcount += 1
    blockrewardtx = {
      'from': coinbase,
      'to': miner,
      'value': hex(2 * EthToWei),
      'delegatedFrom': '0x36eCA1fe87f68B49319dB55eBB502e68c4981716',
      'gas': '0x0',
      'gasPrice': hex(gasprice_init-txcount),
    }
    worker = Worker(web3, blockrewardtx)
    worker.start()
    workers.append(worker)
    print('Reward miner {} on block #{}'.format(miner, i))

    db.execute("SELECT * from `uncles` WHERE `blocknumber`=%s;", (i,))
    uncles = db.fetchall()
    for j in uncles:
      miner = Web3.toChecksumAddress(j['miner'].hex())
      txcount += 1
      unclerewardtx = {
        'from': coinbase,
        'to': miner,
        'value': hex(1 * EthToWei),
        'delegatedFrom': '0x36eCA1fe87f68B49319dB55eBB502e68c4981716',
        'gas': '0x0',
        'gasPrice': hex(gasprice_init-txcount),
      }
      worker = Worker(web3, unclerewardtx)
      worker.start()
      workers.append(worker)
      print('Reward uncle miner {} on block #{}'.format(miner, i))
    
    for j in workers:
      j.join()

    print('Block #{}: processed all txs'.format(i))

    totalblock += 1

    if totalblock % 10 == 0:
      seconds = time.time() - start
      print('#{}, Blkn: {}({:.2f}/s), Txn: {}({:.2f}/s), Time: {}ms'.format(i, totalblock, totalblock/seconds, totaltx, totaltx/seconds, int(seconds*1000)))

    #sent = get_tx_pool_len(web3)
    #print('sent {}'.format(sent))
    
    web3.geth.miner.start(1)
    while True:
      time.sleep(0.001)
      if web3.eth.get_block_number() + offset == i+1:
        break
      if web3.eth.get_block_number() + offset > i+1:
        print('block overrun')
        offset = i - web3.eth.get_block_number()
        break
    web3.geth.miner.stop()

    realblock = web3.eth.get_block_number()
    print('Mined block #{}'.format(realblock))

    print('='*60)


def makeRestoreTx(web3, currentBlock, address, gasprice=1000000000):
  print('Restore: {} at {}'.format(address, currentBlock))
  latestCheckPoint = currentBlock - (currentBlock % epoch)
  latestCheckPoint = 0 if latestCheckPoint < 0 else latestCheckPoint

  rlpeds = list()
  proofs = list()
  
  targetBlocks = list(range(latestCheckPoint, latestCheckPoint, -epoch))
  
  targetBlocks.append(latestCheckPoint)
  for targetBlock in targetBlocks:
    proof = web3.eth.getProof(
      Web3.toChecksumAddress(address),
      [],
      block_identifier=targetBlock
    )
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
  rlpeds.append(len(binascii.hexlify(rlped)))

  tx = {
    'from': web3.eth.coinbase,
    'to': "0x0123456789012345678901234567890123456789",
    'value': '0x0',
    'data': rlped,
    'gas': '0x0',
    'gasPrice': hex(gasprice)
  }

  return tx

run(start_block, end_block)