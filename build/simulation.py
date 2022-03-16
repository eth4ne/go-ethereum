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

db_host = 'localhost'
db_user = 'ethereum'
db_pass = '1234' #fill in the MariaDB/MySQL password.
db_name = 'ethereum'

geth_ipc = '/ethereum/geth-test/geth.ipc'

start_block = 7000000
end_block = 8000000
password = '1234' #fill in the geth coinbase password.

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
    #self.web3.eth.send_transaction(self.tx)
    

def get_tx_pool_len(web3):
  txpool = web3.geth.txpool.inspect()
  pending = sum([len(v) for k, v in txpool['pending'].items()])
  queued = sum([len(v) for k, v in txpool['queued'].items()])
  return pending + queued
  

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

  web3.custom.setAllowZeroTxBlock(False)
  web3.custom.setAllowConsecutiveZeroTxBlock(False)

  for i in range(_from, _to+1):
    db.execute("SELECT * from `transactions` WHERE `blocknumber`=%s ORDER BY `nonce` ASC;", (i,))
    result = db.fetchall()
    print('Block #{}: fetched {} txs'.format(i, len(result)))
    workers = []
    for j in result:
      to = j['to']
      if to != None:
        to = Web3.toChecksumAddress(to.hex())
      tx = {
        'from': coinbase,
        'to': to,
        'value': '0x0',
        'data': '0x'+j['input'].hex(),
        'delegatedFrom': Web3.toChecksumAddress(j['from'].hex()),
        'gas': '0x0',
        'gasPrice': '0x3b9aca00', #1000000000
      }
      worker = Worker(web3, tx)
      worker.start()
      workers.append(worker)
    if len(result) == 0:
      web3.custom.setAllowZeroTxBlock(True)
    else:
      web3.custom.setAllowZeroTxBlock(False)
      
    totaltx += len(result)
    
    for j in workers:
      j.join()

    #print('Block #{}: processed all txs'.format(i))

    totalblock += 1

    if totalblock % 10 == 0:
      seconds = time.time() - start
      print('#{}, Blkn: {}({:.2f}/s), Txn: {}({:.2f}/s), Time: {}ms'.format(i, totalblock, totalblock/seconds, totaltx, totaltx/seconds, int(seconds*1000)))

    sent = get_tx_pool_len(web3)
    print('sent {}'.format(sent))
    
    web3.geth.miner.start(1)
    startjob = time.time()
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
    print('Actual block #{}'.format(realblock))

    print('='*60)

run(start_block, end_block)