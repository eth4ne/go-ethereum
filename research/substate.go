package research

import (
	"bytes"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// SubstateAccount is modification of GenesisAccount in core/genesis.go
type SubstateAccount struct {
	Nonce   uint64
	Balance *big.Int
	Storage map[common.Hash]common.Hash
	Code    []byte
}

func NewSubstateAccount(nonce uint64, balance *big.Int, code []byte) *SubstateAccount {
	return &SubstateAccount{
		Nonce:   nonce,
		Balance: new(big.Int).Set(balance),
		Storage: make(map[common.Hash]common.Hash),
		Code:    code,
	}
}

func (x *SubstateAccount) Equal(y *SubstateAccount) bool {
	if x == y {
		return true
	}

	if (x == nil || y == nil) && x != y {
		return false
	}

	equal := (x.Nonce == y.Nonce &&
		x.Balance.Cmp(y.Balance) == 0 &&
		bytes.Equal(x.Code, y.Code) &&
		len(x.Storage) == len(y.Storage))
	if !equal {
		return false
	}

	for k, xv := range x.Storage {
		yv, exist := y.Storage[k]
		if !(exist && xv == yv) {
			return false
		}
	}

	return true
}

func (sa *SubstateAccount) Copy() *SubstateAccount {
	saCopy := NewSubstateAccount(sa.Nonce, sa.Balance, sa.Code)

	for key, value := range sa.Storage {
		saCopy.Storage[key] = value
	}

	return saCopy
}

func (sa *SubstateAccount) CodeHash() common.Hash {
	return crypto.Keccak256Hash(sa.Code)
}

type SubstateAlloc map[common.Address]*SubstateAccount

func (x SubstateAlloc) Equal(y SubstateAlloc) bool {
	if len(x) != len(y) {
		return false
	}

	for k, xv := range x {
		yv, exist := y[k]
		if !(exist && xv.Equal(yv)) {
			return false
		}
	}

	return true
}

type SubstateEnv struct {
	Coinbase    common.Address
	Difficulty  *big.Int
	GasLimit    uint64
	Number      uint64
	Timestamp   uint64
	BlockHashes map[uint64]common.Hash
}

func NewSubstateEnv(b *types.Block, blockHashes map[uint64]common.Hash) *SubstateEnv {
	var env = &SubstateEnv{}

	env.Coinbase = b.Coinbase()
	env.Difficulty = new(big.Int).Set(b.Difficulty())
	env.GasLimit = b.GasLimit()
	env.Number = b.NumberU64()
	env.Timestamp = b.Time()
	env.BlockHashes = make(map[uint64]common.Hash)
	for num64, bhash := range blockHashes {
		env.BlockHashes[num64] = bhash
	}

	return env
}

func (x *SubstateEnv) Equal(y *SubstateEnv) bool {
	if x == y {
		return true
	}

	if (x == nil || y == nil) && x != y {
		return false
	}

	equal := (x.Coinbase == y.Coinbase &&
		x.Difficulty.Cmp(y.Difficulty) == 0 &&
		x.GasLimit == y.GasLimit &&
		x.Number == y.Number &&
		x.Timestamp == y.Timestamp &&
		len(x.BlockHashes) == len(y.BlockHashes))
	if !equal {
		return false
	}

	for k, xv := range x.BlockHashes {
		yv, exist := y.BlockHashes[k]
		if !(exist && xv == yv) {
			return false
		}
	}

	return true
}

// modification of types.Receipt
type SubstateResult struct {
	Status uint64
	Bloom  types.Bloom
	Logs   []*types.Log

	ContractAddress common.Address
	GasUsed         uint64
}

func NewSubstateResult(receipt *types.Receipt) *SubstateResult {
	var sr = &SubstateResult{}

	sr.Status = receipt.Status
	sr.Bloom = receipt.Bloom
	sr.Logs = receipt.Logs

	sr.ContractAddress = receipt.ContractAddress
	sr.GasUsed = receipt.GasUsed

	return sr
}

func (x *SubstateResult) Equal(y *SubstateResult) bool {
	if x == y {
		return true
	}

	if (x == nil || y == nil) && x != y {
		return false
	}

	equal := (x.Status == y.Status &&
		x.Bloom == y.Bloom &&
		len(x.Logs) == len(y.Logs) &&
		x.ContractAddress == y.ContractAddress &&
		x.GasUsed == y.GasUsed)
	if !equal {
		return false
	}

	for i, xl := range x.Logs {
		yl := y.Logs[i]

		equal := (xl.Address == yl.Address &&
			len(xl.Topics) == len(yl.Topics) &&
			bytes.Equal(xl.Data, yl.Data))
		if !equal {
			return false
		}

		for i, xt := range xl.Topics {
			yt := yl.Topics[i]
			if xt != yt {
				return false
			}
		}
	}

	return true
}

type Substate struct {
	InputAlloc  SubstateAlloc
	OutputAlloc SubstateAlloc
	// Env         *SubstateEnv
	// Message     *SubstateMessage
	// Result      *SubstateResult
}

// func NewSubstate(inputAlloc SubstateAlloc, outputAlloc SubstateAlloc, env *SubstateEnv, message *SubstateMessage, result *SubstateResult) *Substate {
func NewSubstate(inputAlloc SubstateAlloc, outputAlloc SubstateAlloc) *Substate {
	return &Substate{
		InputAlloc:  inputAlloc,
		OutputAlloc: outputAlloc,
		// Env:         env,
		// Message:     message,
		// Result:      result,
	}
}

func (x *Substate) Equal(y *Substate) bool {
	if x == y {
		return true
	}

	if (x == nil || y == nil) && x != y {
		return false
	}

	equal := (x.InputAlloc.Equal(y.InputAlloc) &&
		x.OutputAlloc.Equal(y.OutputAlloc))
	// x.OutputAlloc.Equal(y.OutputAlloc) &&
	// x.Env.Equal(y.Env) &&
	// x.Message.Equal(y.Message) &&
	// x.Result.Equal(y.Result))
	return equal
}
