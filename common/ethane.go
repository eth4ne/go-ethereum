package common

// (jhkim)

import (
	"math/big"
	"sync"
)

var (
	// GlobalDistance                int     = 0
	GlobalTxHash                  Hash    = HexToHash("0x0")
	GlobalTxTo                    Address = HexToAddress("0x0")
	GlobalTxFrom                  Address = HexToAddress("0x0")
	GlobalBlockNumber             int     = 0
	GlobalMutex                   sync.Mutex
	GlobalBlockMiner              Address = HexToAddress("0x0")
	GlobalBlockUncles                     = []Address{}
	GlobalDeployedContractAddress Address = HexToAddress("0x0")

	TxDetail = map[Hash]*TxInformation{} // key : TxID, value: struct common.TxInformation

	TxSubstate     = map[int](map[Hash]SubstateAlloc){} // key: block number, value: map(key: tx hash, value: SubstateAlloc)
	BlockTxList    = map[int][]Hash{}                   // key: block number, value: tx hash
	BlockMinerList = map[int]SimpleAccount{}            // key: block number, value: Address of block miner

	BlockUncleList = map[int][]SimpleAccount{} // key: block number, value: Addresses of block uncles

	TxReadList  = map[Hash]SubstateAlloc{} // key: tx hash, value: SubstateAlloc(map key:address, value:stateAccount)
	TxWriteList = map[Hash]SubstateAlloc{} // key: tx hash, value: SubstateAlloc(map key:address, value:stateAccount)

)

type SimpleAccount struct {
	Addr Address

	Nonce       uint64
	Balance     *big.Int
	Codehash    Hash
	StorageRoot Hash
}

type SubstateAlloc map[Address]*SubstateAccount

type TxInformation struct {
	From                    Address
	To                      Address
	Types                   int // 1: send, 2: contract creation, 3: contract call, 4: failed, 0: default.
	DeployedContractAddress Address

	// CA_write_slotHash map[Address]*[]Hash // key: contract address, value: written slothashes in tx
	// CA_read_slotHash  map[Address]*[]Hash // key: contract address, value: read(touched) slothashes in tx

}

// SubstateAccount is modification of GenesisAccount in core/genesis.go
type SubstateAccount struct {
	Nonce   uint64
	Balance *big.Int
	// Storage map[Hash]Hash
	// Storage map[Hash][]Hash // To keep tracking changes of slot values, use hash list rather than hash
	Storage [](map[Hash]Hash) // make order of changes of slot values, list of maps (key: slot, value: hex value)
	Code    []byte

	StorageRoot Hash
	// CodeHash    Hash
	CodeHash []byte
}

func NewSubstateAccount(nonce uint64, balance *big.Int, code []byte) *SubstateAccount {
	return &SubstateAccount{
		Nonce:   nonce,
		Balance: new(big.Int).Set(balance),
		// Storage: make(map[Hash]Hash),
		// Storage: make(map[Hash][]Hash),
		Storage: [](map[Hash]Hash){},
		Code:    code,
	}
}

func MyGetStorage(storage [](map[Hash]Hash), slot Hash) (Hash, bool) {

	for _, m := range storage { // 이거 뒤에서부터 봐야할거같은데?
		// fmt.Println("  ContainStorage/ index:", i)
		if value, exist := m[slot]; exist {
			return value, true
		}
	}
	return Hash{}, false
}
