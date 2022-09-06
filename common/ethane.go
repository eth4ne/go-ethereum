package common

// (jhkim)

import (
	"sync"
)

var (
	GlobalDistance    int     = 0
	GlobalTxHash      Hash    = HexToHash("0x0")
	GlobalTxTo        Address = HexToAddress("0x0")
	GlobalTxFrom      Address = HexToAddress("0x0")
	GlobalBlockNumber int     = 0
	GlobalMutex       sync.Mutex
	GlobalBlockMiner  Address = HexToAddress("0x0")
	GlobalBlockUncles         = []Address{}
	// GlobalBlockUnclesHeader         = []Address{}
	GlobalDeployedContractAddress Address = HexToAddress("0x0")
	ZeroBlockTI                           = TxInformation{}
	MinerUnlce                            = map[int]map[Address]uint64{} // key: block number, value: map[key: address of miner and uncles, value: last balance]

	Flushednode_block = map[int](map[Hash]int){} // key : block number, value: list of node hash
	FlushedNodeList   = map[Hash]int{}           // key : block number, value: list of node hash

	FlushedNodeDuplicate_block = map[Hash][]int{} // key : block number, value: map [key: duplicated node hash, value: count]
	FlushedNodeDuplicate       = map[Hash]int{}   // 1: already existed

	AddrHash2Addr = map[Hash]Address{} // key : AddressHash, value: Ethereum Address
	// Account_SlotHash2Slot = map[Address]([][][]byte){} // key : Contract account address, value: bytes array of slothash and slot and value and blocknumber
	Account_SlotHash2Slot = map[Address]([]SlotDetail){} // key : Contract account address, value: bytes array of slothash and slot and value and blocknumber
	// AddrHash2AddrSyncMap = sync.Map{} // key : AddressHash, value: Ethereum Address

	TxDetail = map[Hash]*TxInformation{} // key : TxID, value: struct common.TxInformation
	// TxDetailSyncMap    = sync.Map{}
	// TrieUpdateElse     = sync.Map{} // include miner and else // key: blocknumber, value: list of Address
	TrieUpdateElse     = map[int][]Address{} // include miner and else // key: blocknumber, value: list of Address
	TrieUpdateElseTemp = []Address{}         // unknown account update. zeroblock

	Coinbase Address = HexToAddress("0xc4422d1C18E9Ead8A9bB98Eb0d8bB9dbdf2811D7")
	// TxDetailHashmap = map[string]([]Hash){}    // key : TxID(temporaliy set string), value: addressHash List{from , to, isCA(boolean)}
	// CASlotHash      = map[Hash][]Hash{}        // key : CA addressHash

	IsPrefetch bool = false
)

type SlotDetail struct {
	Hashkey     Hash
	Key         Address
	Value       []byte
	Blocknumber int
}

type TxInformation struct {
	BlockNumber             int64
	From                    Address
	To                      Address
	FromAdrHash             Hash
	ToAdrHash               Hash
	Types                   int       // 1: send, 2: contract creation, 3: contract call, 4: failed, 0: default.
	Internal                []Address // list of EOA those balance is modified by internal tx
	InternalCA              []Address // list of CA those balance is modified by internal tx
	DeployedContractAddress Address
	AccountBalance          map[Address]uint64

	ContractAddress_SlotHash map[Address]*[]Hash // key: contract address, value: ca's slotHashes
	SlotHash                 []Hash
}
