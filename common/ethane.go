package common

// (jhkim)

import (
	"sync"
)

var (
	GlobalTxHash      Hash    = HexToHash("0x0")
	GlobalTxTo        Address = HexToAddress("0x0")
	GlobalTxFrom      Address = HexToAddress("0x0")
	GlobalBlockNumber int     = 0
	GlobalMutex       sync.Mutex
	GlobalBlockMiner  Address = HexToAddress("0x0")
	GlobalBlockUncles         = []Address{}
	// GlobalBlockUnclesHeader         = []Address{}
	GlobalContractAddress Address = HexToAddress("0x0")

	Flushednode_block = map[int]*(map[Hash]int){} // key : block number, value: list of node hash
	FlushedNodeList   = map[Hash]int{}            // key : block number, value: list of node hash

	FlushedNodeDuplicate_block = map[Hash][]int{} // key : block number, value: map [key: duplicated node hash, value: count]
	FlushedNodeDuplicate       = map[Hash]int{}   // 1: already existed

	AddrHash2Addr = map[Hash]Address{} // key : AddressHash, value: Ethereum Address
	// AddrHash2AddrSyncMap = sync.Map{} // key : AddressHash, value: Ethereum Address

	TxDetail = map[Hash]*TxInformation{} // key : TxID, value: struct common.TxInformation
	// TxDetailSyncMap    = sync.Map{}
	// TrieUpdateElse     = sync.Map{} // include miner and else // key: blocknumber, value: list of Address
	TrieUpdateElse     = map[int]*[]Address{} // include miner and else // key: blocknumber, value: list of Address
	TrieUpdateElseTemp = []Address{}

	Coinbase Address = HexToAddress("0xc4422d1C18E9Ead8A9bB98Eb0d8bB9dbdf2811D7")
	// TxDetailHashmap = map[string]([]Hash){}    // key : TxID(temporaliy set string), value: addressHash List{from , to, isCA(boolean)}
	// CASlotHash      = map[Hash][]Hash{}        // key : CA addressHash
)

type TxInformation struct {
	BlockNumber             int64
	From                    Address
	To                      Address
	FromAdrHash             Hash
	ToAdrHash               Hash
	Types                   int       // 1: send, 2: contract creation, 3: contract call, 4: failed, 0: default.
	Else                    []Address // account list modified by internal tx
	DeployedContractAddress Address

	ContractAddress_SlotHash map[Address]*[]Hash // key: contract address, value: ca's slotHashes
	SlotHash                 []Hash
}
