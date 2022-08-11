## TxSubstate Recorder

Geth for recording TxSubstate and details of storage.
* Default file path uses absolute address in `lynx`
* Record files of TxSubstates saved in ./txDetail



## Example of TxSubstate
```
/Block:79422

TxHash:0x916bcfc0cbdf2f2de456a3156e915c0f3d9cbc78ccc453d50ed105150cacec07
  Type:ContractCall
  From:0xD1826373C4E0938f0b4302B40324f7f9c546492C
  To:0xc634A6B0375D5554116900D0B40a06C2C022f17e

  ReadList
    address:0xc634A6B0375D5554116900D0B40a06C2C022f17e
    address:0xe090Ffdced499691fA9379752a59F8A058c1eE4A
    address:0xD1826373C4E0938f0b4302B40324f7f9c546492C
  WriteList
    address:0xc634A6B0375D5554116900D0B40a06C2C022f17e
      Nonce:0
      Balance:200000000000000000
      CodeHash:0xa86baac891cc174ca49cf01c54e41742801f36aaf3b633251552badeeae2cd80
      StorageRoot:0x9e6bffa1282cb538ea6051799fd0c4011adecf50fb221963d103c5f86b9e265a
        Storage:
          slot:0x0000000000000000000000000000000000000000000000000000000000000008,value:0x01
          slot:0x0000000000000000000000000000000000000000000000000000000000000003,value:0x55cc8f16
          slot:0x0000000000000000000000000000000000000000000000000000000000000004,value:0x55cc8f16
          slot:0x0000000000000000000000000000000000000000000000000000000000000002,value:0xd1826373c4e0938f0b4302b40324f7f9c546492c
          slot:0x0000000000000000000000000000000000000000000000000000000000000003,value:0x55cc8f16
      RlpEncoded:0xf84c808802c68af0bb140000a09e6bffa1282cb538ea6051799fd0c4011adecf50fb221963d103c5f86b9e265aa0a86baac891cc174ca49cf01c54e41742801f36aaf3b633251552badeeae2cd80
    address:0xe090Ffdced499691fA9379752a59F8A058c1eE4A
      Nonce:0
      Balance:189425774586116133769
      CodeHash:0xc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470
      StorageRoot:0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421
      RlpEncoded:0xf84d80890a44cf8a5d84ab5389a056e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470
    address:0xD1826373C4E0938f0b4302B40324f7f9c546492C
      Nonce:23
      Balance:1038017569422630206
      CodeHash:0xc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470
      StorageRoot:0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421
      RlpEncoded:0xf84c17880e67c77ad00cdd3ea056e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470


Miner:0xe090Ffdced499691fA9379752a59F8A058c1eE4A
  Nonce:0
  Balance:194425774586116133769
  Codehash:0xc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470
  StorageRoot:0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421
  RlpEncoded:0xf84d80890a8a331bdfc99f5389a056e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470

```
## Change epoch of TxSubstate.txt

At the end of writeBlockAndSetHead in `./core/blockchain.go`, change variable `distance` what you want.   


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
If you want to quit geth at specific block, set common.GlobalBlocknumber to that block number

```Go

	if common.GlobalBlockNumber == 500001 {
		os.Exit(0)
	}

```
