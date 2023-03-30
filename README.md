## Ethereum Tx data recorder
<!-- A Geth client for recording TxSubstates and details of storage slot read/write. -->
A [Geth](https://github.com/ethereum/go-ethereum) client that records `txSubstate` which includes read and write information of txsubstates and storage slots during full sync.


### TxSubstate
`TxSubstate` is a detailed information of Ethereum transactions including not only `Txhash`, `Type`, `To`, `From`, which are available in [Etherscan](https://etherscan.io/), but also accurate `Readlist`, `Writelist` of storage slots in a easily parseable format.




### Run client
* Set `DataDir` in `./build/bin/sync.sh` where Ethereum levelDB saved
* Set `Path` in `./common/ethane.go` where `TxSubstates` will be saved, then:
```sh
$ sh ./build/bin/sync.sh
```




### Example of TxSubstate
```
/Block:79422
!TxHash:0x916bcfc0cbdf2f2de456a3156e915c0f3d9cbc78ccc453d50ed105150cacec07
Type:ContractCall
From:0xD1826373C4E0938f0b4302B40324f7f9c546492C
To:0xc634A6B0375D5554116900D0B40a06C2C022f17e
@ReadList
.address:0xc634A6B0375D5554116900D0B40a06C2C022f17e
.address:0xD1826373C4E0938f0b4302B40324f7f9c546492C
.address:0xe090Ffdced499691fA9379752a59F8A058c1eE4A
#WriteList
.address:0xD1826373C4E0938f0b4302B40324f7f9c546492C
Nonce:23
Balance:1038017569422630206
CodeHash:empty
StorageRoot:empty
.address:0xc634A6B0375D5554116900D0B40a06C2C022f17e
Nonce:0
Balance:200000000000000000
CodeHash:0xa86baac891cc174ca49cf01c54e41742801f36aaf3b633251552badeeae2cd80
StorageRoot:0x9e6bffa1282cb538ea6051799fd0c4011adecf50fb221963d103c5f86b9e265a
Storage:
slot:0x0000000000000000000000000000000000000000000000000000000000000008,value:0x01
slot:0x0000000000000000000000000000000000000000000000000000000000000003,value:0x55cc8f16
slot:0x0000000000000000000000000000000000000000000000000000000000000004,value:0x55cc8f16
slot:0x0000000000000000000000000000000000000000000000000000000000000002,value:0xd1826373c4e0938f0b4302b40324f7f9c546492c
.address:0xe090Ffdced499691fA9379752a59F8A058c1eE4A
Nonce:0
Balance:189425774586116133769
CodeHash:empty
StorageRoot:empty
$Miner:0xe090Ffdced499691fA9379752a59F8A058c1eE4A
Nonce:0
Balance:194425774586116133769
CodeHash:empty
StorageRoot:empty
```
### Change epoch of TxSubstate.txt

At the end of function `writeBlockAndSetHead` in `./core/blockchain.go`, change variable `distance` what you want.   

```Go
func (bc *BlockChain) writeBlockAndSetHead(block *types.Block, receipts []*types.Receipt, logs []*types.Log, state *state.StateDB, emitHeadEvent bool) (status WriteStatus, err error) {
	...
	var distance = 500000

	// fmt.Println("  state_processor.go", blocknumber, distance, common.GlobalDistance)
	if common.GlobalBlockNumber%distance == 0 && common.GlobalBlockNumber != 0 {
		PrintTxSubstate(common.GlobalBlockNumber, distance)
		fmt.Println("DONE blocknumber:", common.GlobalBlockNumber)

		// reset TxReadList and TxWriteList
		common.TxDetail = make(map[common.Hash]*common.TxInformation)
		common.TxReadList = make(map[common.Hash]common.SubstateAlloc)
		common.TxWriteList = make(map[common.Hash]common.SubstateAlloc)
		common.BlockMinerList = make(map[int]common.SimpleAccount)
		common.BlockUncleList = make(map[int][]common.SimpleAccount)
		common.BlockTxList = make(map[int][]common.Hash)
	}
   ...
}
```
If you want to quit client at specific block, set `common.GlobalBlocknumber` to that block number

```Go
// example for shutdown where before 500,001 block is finalized
if common.GlobalBlockNumber == 500001 {
  os.Exit(0)
}
```

### Parse TxSubstates
go [eth-analysis](https://github.com/eth4ne/eth-analysis) and execute Python files to parse txsubstates and insert them into a MariaDB database
```sh
$ python3 state_slot_init.py
$ python3 txsubstate.py -s {startBlockNum} -e {endBlockNum} -i {interval} -d ~/{path}/{to}/{txsubstates}
```
