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
  HexStr
)

import pymysql.cursors
import time
import threading
import json
import rlp
import binascii
import traceback

db_host = 'localhost'
db_user = 'ethereum'
db_pass = '1234' #fill in the MariaDB/MySQL password.
db_name = 'ethereum'

geth_ipc = '/ethereum/hletrd/data/geth.ipc' #fill in the IPC path.

start_block = 1
end_block = 46146
epoch = 315
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
restore_mode = RESTORE_OLDEST

restorefile = 'restore_1_1000000_315.json'

time.sleep(1)

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

  setBlockParameters: Method[Callable[[HexStr], None]] = Method(
    RPCEndpoint("eth_setBlockParameters"),
    mungers=[default_root_munger],
  )

class Worker(threading.Thread):
  def __init__(self, web3, tx):
    threading.Thread.__init__(self)
    self.web3 = web3
    self.tx = tx
  
  def run(self):
    try:
      self.web3.eth.send_transaction(self.tx)
    except:
      traceback.print_exc()
      print(self.tx)

class ParameterWorker(threading.Thread):
  def __init__(self, web3, parameters):
    threading.Thread.__init__(self)
    self.web3 = web3
    self.parameters = parameters
  
  def run(self):
    try:
      self.web3.custom.setBlockParameters(self.parameters)
    except:
      traceback.print_exc()
      print(self.parameters)

delay = 0

def adaptive_sleep_init(start_delay=0.0001):
  global delay
  delay = start_delay

def adaptive_sleep():
  global delay
  if delay < 1:
    delay *= 1.1
  else:
    delay *= 2
  time.sleep(delay)

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

  web3.geth.miner.stop()
  web3.debug.setHead(hex(_from-1))
  print('Rewinding head to {}'.format(_from-1))

  coinbase = web3.eth.coinbase
  web3.geth.personal.unlock_account(coinbase, password, 0)

  print('Account {} unlocked'.format(coinbase))
  print('Run from {} to {}'.format(_from, _to))

  offset = 0 # - 1
  realblock = 0

  with open(restorefile, 'r') as f:
    restoredata = f.read()
    restoredata = json.loads(restoredata)

  web3.custom.setAllowZeroTxBlock(False)
  web3.custom.setAllowConsecutiveZeroTxBlock(False)

  prevminer = '0x0000000000000000000000000000000000000000'

  i = _from
  while i <= _to:
    workers = []

    db.execute("SELECT * from `blocks` WHERE `number`=%s;", (i,))
    blocks = db.fetchone()
    #print('Fetched headers of block #{}'.format(i))

    blockparameter = '0x'
    blockparameter += '{:016x}'.format(blocks['timestamp'])
    blockparameter += '{:040x}'.format(blocks['difficulty'])
    blockparameter += blocks['nonce'].hex()
    blockparameter += '{:016x}'.format(blocks['gaslimit'])
    blockparameter += blocks['mixhash'].hex()
    blockparameter += blocks['miner'].hex()
    blockparameter += blocks['hash'].hex()
    blockparameter += blocks['extradata'].hex()

    parameterworker = ParameterWorker(web3, blockparameter)
    parameterworker.start()
    workers.append(parameterworker)

    miner = blocks['miner'].hex()
    blockrewardtx = {
      'from': coinbase,
      'to': '0x36eCA1fe87f68B49319dB55eBB502e68c4981716',
      'value': '0x0',
      'data': miner,
      'gas': '0x0',
      'gasPrice': '0xff',
    }
    worker = Worker(web3, blockrewardtx)
    worker.start()
    workers.append(worker)
    print('Reward miner {} on block #{}'.format(miner, i))
    prevminer = blocks['miner'].hex()

    timestamp = blocks['timestamp']
    timestamptx = {
      'from': coinbase,
      'to': '0xf1454994a012Dfad6d65957dBf47fe28c2dC97A2',
      'value': '0x0',
      'data': '0x{:016x}'.format(timestamp),
      'gas': '0x0',
      'gasPrice': '0xff',
    }
    worker = Worker(web3, timestamptx)
    worker.start()
    workers.append(worker)
    print('Set timestamp as {} on block #{}'.format(timestamp, i))

    difficulty = blocks['difficulty']
    difficultytx = {
      'from': coinbase,
      'to': '0x50F2ca12BA5aF79aa03D5dC382572973A9C0adA8',
      'value': '0x0',
      'data': '0x{:016x}'.format(difficulty),
      'gas': '0x0',
      'gasPrice': '0xff',
    }
    worker = Worker(web3, difficultytx)
    worker.start()
    workers.append(worker)
    print('Set difficulty as {} on block #{}'.format(difficulty, i))

    nonce = blocks['nonce']
    noncetx = {
      'from': coinbase,
      'to': '0x0A258FF4194E8c135F2C23a16EDf3d8d91Ba0805',
      'value': '0x0',
      'data': '0x'+nonce.hex(),
      'gas': '0x0',
      'gasPrice': '0xff',
    }
    worker = Worker(web3, noncetx)
    worker.start()
    workers.append(worker)
    print('Set nonce as 0x{} on block #{}'.format(nonce.hex(), i))

    gaslimit = blocks['gaslimit']
    gaslimittx = {
      'from': coinbase,
      'to': '0x91C656fE89dB9eb9FB6f5002613EE3754542D541',
      'value': '0x0',
      'data': '0x{:016x}'.format(gaslimit),
      'gas': '0x0',
      'gasPrice': '0xff',
    }
    worker = Worker(web3, gaslimittx)
    worker.start()
    workers.append(worker)
    print('Set gaslimit as {} on block #{}'.format(gaslimit, i))

    extradata = blocks['extradata']
    extradatatx = {
      'from': coinbase,
      'to': '0x24e66Ef7d2EFE1b0eB194eB30955b0ed64A9F615',
      'value': '0x0',
      'data': '0x'+extradata.hex(),
      'gas': '0x0',
      'gasPrice': '0xff',
    }
    worker = Worker(web3, extradatatx)
    worker.start()
    workers.append(worker)
    print('Set extradata as 0x{} on block #{}'.format(extradata.hex(), i))

    mixhash = blocks['mixhash']
    mixhashtx = {
      'from': coinbase,
      'to': '0x10f9358800B5346BEb25390290aF3487ecDa2e62',
      'value': '0x0',
      'data': '0x'+mixhash.hex(),
      'gas': '0x0',
      'gasPrice': '0xff',
    }
    worker = Worker(web3, mixhashtx)
    worker.start()
    workers.append(worker)
    print('Set mixhash as 0x{} on block #{}'.format(mixhash.hex(), i))

    db.execute("SELECT * from `uncles` WHERE `blocknumber`=%s;", (i,))
    uncles = db.fetchall()
    for j in uncles:
      miner = j['miner'].hex()
      extradata = j['extradata']
      extradata = extradata.rstrip(b'\x00')
      uncledata = '0x'+miner
      uncledata += '{:016x}'.format(j['difficulty'])
      uncledata += '{:016x}'.format(j['gaslimit'])
      uncledata += '{:016x}'.format(j['gasused'])
      uncledata += j['mixhash'].hex()
      uncledata += j['nonce'].hex()
      uncledata += j['sha3uncles'].hex()
      uncledata += '{:016x}'.format(j['timestamp'])
      uncledata += j['receiptsroot'].hex()
      uncledata += j['transactionsroot'].hex()
      uncledata += j['stateroot'].hex()
      uncledata += extradata.hex()
      
      unclerewardtx = {
        'from': coinbase,
        'to': '0xb3711B7e50Fe9Ff914ec0F08C6b8330a41E93C10',
        'value': hex(j['uncleheight']),
        'data': uncledata,
        'gas': '0x0',
        'gasPrice': '0xff',
      }
      worker = Worker(web3, unclerewardtx)
      worker.start()
      worker.join() #process uncle sequentially (for proper ordering)
      workers.append(worker)
      #print('Reward uncle miner {} of height {} on block #{}'.format(miner, j['uncleheight'], i))


    db.execute("SELECT * from `transactions` WHERE `blocknumber`=%s;", (i,))
    result = db.fetchall()
    txcount = len(result)
    #print('Block #{}: fetched {} txs'.format(i, txcount))

    order = 0

    if str(i-restore_offset) in restoredata:
      for j in restoredata[str(i-restore_offset)]:
        tx = makeRestoreTx(web3, i-restore_offset - offset, j, 100, prevminer, order)
        order += 1
        worker = Worker(web3, tx)
        worker.start()
        workers.append(worker)

      restorecount = len(restoredata[str(i-restore_offset)])
      print('Block #{}: Restored {} accounts'.format(i, ))
      txcount += restorecount

    for j in result:
      to = j['to']
      if to != None:
        to = to.hex()
        to = Web3.toChecksumAddress(to)
      else:
        to = ''
      tx = {
        'from': coinbase,
        'to': to,
        'value': hex(int(j['value'])),
        'gas': hex(int(j['gas'])),
        'gasPrice': hex(int(j['gasprice'])),
        'nonce': hex(int(j['nonce'])),
        'data': '',
      }

      tx['data'] += j['v'].hex()
      tx['data'] += j['r'].hex()
      tx['data'] += j['s'].hex()
      tx['data'] += '{:08x}'.format(j['transactionindex'] + order)
      tx['data'] += j['from'].hex()
      tx['data'] += '9669e84351a57aa8a3cfd02001acc246b982713a' #magic header

      if execution_mode == MODE_ETHANE:
        tx['data'] += j['input'].hex()
      elif execution_mode == MODE_ETHANOS:
        if to == None:
          db.execute("SELECT * from `contracts` WHERE `creationtx`=%s;", (j['hash'],))
          result = db.fetchall()
          tx['to'] = result[0]['address'].hex()
      worker = Worker(web3, tx)
      worker.start()
      workers.append(worker)
      
    totaltx += len(result)
    
    for j in workers:
      j.join()

    if len(result) == 0:
      web3.custom.setAllowZeroTxBlock(True)
    else:
      web3.custom.setAllowZeroTxBlock(False)

    adaptive_sleep_init(0.0001)
    while int(web3.geth.txpool.status()['pending'], 16) < txcount:
      adaptive_sleep()

    #print('Block #{}: processed all txs'.format(i))
    totalblock += 1

    if totalblock % 100 == 0:
      seconds = time.time() - start
      print('#{}, Blkn: {}({:.2f}/s), Txn: {}({:.2f}/s), Time: {}ms'.format(i, totalblock, totalblock/seconds, totaltx, totaltx/seconds, int(seconds*1000)))

    web3.geth.miner.start(1)
    adaptive_sleep_init(0.0001)

    rewind = False

    while True:
      adaptive_sleep()
      if web3.eth.get_block_number() + offset == i:
        break
      if web3.eth.get_block_number() + offset > i:
        print('block overrun')
        print('block: {}, offset: {}, i: {}'.format(web3.eth.get_block_number(), offset, i))
        web3.geth.miner.stop()
        time.sleep(0.2)
        web3.debug.setHead(hex(i-1))
        print('Rewinding head to {}'.format(i-1))
        time.sleep(0.2)
        i = i - 1
        rewind = True
        break
    web3.geth.miner.stop()

    if rewind == True:
      continue

    realblock = web3.eth.get_block_number()
    #print('Mined block #{}'.format(realblock))

    stateroot = blocks['stateroot']
    block_made = web3.eth.get_block(i)
    if stateroot != block_made['stateRoot']:
      print('Block #{}: state root mismatch'.format(i))
      print('Expected: {}'.format(stateroot.hex()))
      print('Got: {}'.format(block_made['stateRoot'].hex()))
      print('Rewinding head to {}'.format(i-1))
      time.sleep(0.2)
      web3.debug.setHead(hex(i-1))
      time.sleep(0.2)
      #i = i
      continue
    if blocks['hash'] != block_made['hash']:
      print('Block #{}: hash mismatch'.format(i))
      print('Expected: {}'.format(blocks['hash'].hex()))
      print('Got: {}'.format(block_made['hash'].hex()))
      print('Rewinding head to {}'.format(i-1))
      exit(1)
      time.sleep(0.2)
      web3.debug.setHead(hex(i-1))
      time.sleep(0.2)
      #i = i
      continue
      
    i = i + 1
    #print('='*60)


def makeRestoreTx(web3, currentBlock, address, gasprice, fromaddr, order):
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
      # print("nil proof")
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
  rlpeds.append(len(binascii.hexlify(rlped)))

  data = fromaddr + '{:08x}'.format(order) + rlped.hex()

  tx = {
    'from': web3.eth.coinbase,
    'to': "0x0123456789012345678901234567890123456789",
    'value': '0x0',
    'data': data,
    'gas': '0x0',
    'gasPrice': hex(gasprice)
  }

  return tx

run(start_block, end_block)