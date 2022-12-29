package common

// (jhkim)

import (
	"fmt"
	"math/big"
	"os"
	"sync"
)

// var Path = "/home/jhkim/go/src/github.com/ethereum/go-ethereum-substate/txDetail/" // used absolute path
// var Path = "/home/jhkim/go/src/github.com/ethereum/go-ethereum/txDetail/"
var Path = "/home/jhkim/ethane/"

var (
	// GlobalDistance                int     = 0
	GlobalTxHash      Hash    = HexToHash("0x0")
	GlobalTxTo        Address = HexToAddress("0x0")
	GlobalTxFrom      Address = HexToAddress("0x0")
	GlobalBlockNumber int     = 0
	GlobalMutex       sync.Mutex
	GlobalBlockMiner  Address = HexToAddress("0x0")
	GlobalBlockUncles         = []Address{}

	TxDetail = map[Hash]*TxInformation{} // key : TxID, value: struct common.TxInformation

	// TxSubstate     = map[int](map[Hash]SubstateAlloc){} // key: block number, value: map(key: tx hash, value: SubstateAlloc)
	BlockTxList    = map[int][]Hash{}        // key: block number, value: tx hash
	BlockMinerList = map[int]SimpleAccount{} // key: block number, value: Address of block miner

	BlockUncleList = map[int][]SimpleAccount{} // key: block number, value: Addresses of block uncles

	// TxReadList = map[Hash]SubstateAlloc{} // key: tx hash, value: SubstateAlloc(map key:address, value:stateAccount)
	// TxReadList  = map[Hash][]Address{}     // key: tx hash, value: SubstateAlloc(map key:address, value:empty)

	TxReadList  = map[Hash]map[Address]struct{}{}
	TxWriteList = map[Hash]map[Address]*SubstateAccount{} // key: tx hash, value: SubstateAlloc(map key:address, value:stateAccount)

	HardFork = map[Address]*SubstateAccount{}
)

var (
	// emptyRoot is the known root hash of an empty trie.
	EmptyRoot     = HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
	EmptyCodeHash = HexToHash("c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470")
)

type SimpleAccount struct {
	Addr Address

	Nonce       uint64
	Balance     *big.Int
	Codehash    Hash
	StorageRoot Hash
}

// type SubstateAlloc map[Address]*SubstateAccount

type TxInformation struct {
	From Address
	To   Address

	//tx type: 1: send, 2: contract creation, 3: contract call, 4: temp failed, 41: transfer fail , 42: contract deploy fail, 43: contract call fail, 0: default.
	Types                   int
	DeployedContractAddress Address
	InternalDeployedAddress []Address
	DeletedAddress          []Address
}

// SubstateAccount is modification of GenesisAccount in core/genesis.go
type SubstateAccount struct {
	Nonce   uint64
	Balance *big.Int
	Storage map[Hash]Hash
	// Storage map[Hash][]Hash // To keep tracking changes of slot values, use hash list rather than hash
	// Storage [](map[Hash]Hash) // make order of changes of slot values, list of maps (key: slot, value: hex value)
	Code []byte

	StorageRoot Hash
	CodeHash    Hash
	// CodeHash []byte
}

func NewSubstateAccount(nonce uint64, balance *big.Int, code []byte, storageRoot Hash) *SubstateAccount {
	return &SubstateAccount{
		Nonce:       nonce,
		Balance:     new(big.Int).Set(balance),
		Storage:     make(map[Hash]Hash),
		StorageRoot: storageRoot,
		// Storage: make(map[Hash][]Hash),
		// Storage: [](map[Hash]Hash){},
		Code:     code,
		CodeHash: EmptyCodeHash,
	}
}

func PrettyTxWritePrint(txhash Hash, addr Address) {
	if addr != HexToAddress("0x61C5E2A298f40DBB2adEE3b27C584AdAD6833BaC") {
		return
	}

	if TxWriteList[txhash] == nil {
		fmt.Println("  Pretty TxWrite Print Error: no txhash exists/", txhash)
		os.Exit(0)
	} else if TxWriteList[txhash][addr] == nil {
		fmt.Println("  Pretty TxWrite Print Error: no txhash exists/ txhash", txhash, ", address:", addr)
		os.Exit(0)
	} else {
		fmt.Println()
		fmt.Println("=============================common.TxWriteList=============================")
		fmt.Println("  Txhash:", txhash)
		fmt.Println("  Address:", addr)
		sa := TxWriteList[txhash][addr]
		fmt.Println("    Balance:", sa.Balance)
		fmt.Println("    Nonce:", sa.Nonce)
		if sa.CodeHash != EmptyCodeHash {
			fmt.Println("    CodeHash:", sa.CodeHash)
		} else {
			fmt.Println("    CodeHash: empty")
		}
		fmt.Println("    Code Length:", len(sa.Code))
		if sa.StorageRoot != EmptyRoot {
			fmt.Println("    StorageRoot:", sa.StorageRoot)
		} else {
			fmt.Println("    StorageRoot: empty")
		}
		if len(sa.Storage) != 0 {
			fmt.Println("    Storage:")
			for k, v := range sa.Storage {
				fmt.Println("      slot:", k, "val:", v)
			}
		}
		fmt.Println("=============================================================================")

	}

}
