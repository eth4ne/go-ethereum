// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

// StateProcessor is a basic Processor, which takes care of transitioning
// state from one point to another.
//
// StateProcessor implements Processor.
type StateProcessor struct {
	config *params.ChainConfig // Chain configuration options
	bc     *BlockChain         // Canonical block chain
	engine consensus.Engine    // Consensus engine used for block rewards
}

// NewStateProcessor initialises a new StateProcessor.
func NewStateProcessor(config *params.ChainConfig, bc *BlockChain, engine consensus.Engine) *StateProcessor {
	return &StateProcessor{
		config: config,
		bc:     bc,
		engine: engine,
	}
}

// Process processes the state changes according to the Ethereum rules by running
// the transaction messages using the statedb and applying any rewards to both
// the processor (coinbase) and any included uncles.
//
// Process returns the receipts and logs accumulated during the process and
// returns the amount of gas that was used in the process. If any of the
// transactions failed to execute due to insufficient gas it will return an error.
func (p *StateProcessor) Process(block *types.Block, statedb *state.StateDB, cfg vm.Config) (types.Receipts, []*types.Log, uint64, error) {
	var (
		receipts    types.Receipts
		usedGas     = new(uint64)
		header      = block.Header()
		blockHash   = block.Hash()
		blockNumber = block.Number()
		allLogs     []*types.Log
		gp          = new(GasPool).AddGas(block.GasLimit())
	)
	// Mutate the block and state according to any hard-fork specs
	if p.config.DAOForkSupport && p.config.DAOForkBlock != nil && p.config.DAOForkBlock.Cmp(block.Number()) == 0 {
		misc.ApplyDAOHardFork(statedb)

		// jhkim: DAO account는 초반에 attack에 의해 생긴 account라고 보임
		// stage1-substate: Finalise all DAO accounts, don't save them in substate
		if config := p.config; config.IsByzantium(header.Number) {
			statedb.Finalise(true)
		} else {
			statedb.Finalise(config.IsEIP158(header.Number))
		}

	}
	blockContext := NewEVMBlockContext(header, p.bc, nil)
	vmenv := vm.NewEVM(blockContext, vm.TxContext{}, statedb, p.config, cfg)

	blocknumber := int(block.Number().Int64())

	//jhkim
	common.GlobalBlockNumber = int(block.Number().Int64()) //jhkim
	common.GlobalBlockMiner = block.Coinbase()
	if common.TxReadList == nil {
		common.TxReadList = make(map[common.Hash]common.SubstateAlloc)
		common.TxWriteList = make(map[common.Hash]common.SubstateAlloc)
	}

	if common.BlockMinerList == nil {
		common.BlockMinerList = map[int]common.SimpleAccount{}
	}
	if common.BlockUncleList == nil {
		common.BlockUncleList = map[int][]common.SimpleAccount{}
	}
	if common.BlockTxList == nil {
		common.BlockTxList = map[int][]common.Hash{}
	}
	if common.BlockTxList[common.GlobalBlockNumber] == nil {
		common.BlockTxList[common.GlobalBlockNumber] = []common.Hash{}
	}
	// Iterate over and process the individual transactions
	for i, tx := range block.Transactions() {
		common.TxReadList[tx.Hash()] = common.SubstateAlloc{}
		common.TxWriteList[tx.Hash()] = common.SubstateAlloc{}

		common.BlockTxList[common.GlobalBlockNumber] = append(common.BlockTxList[common.GlobalBlockNumber], tx.Hash())
		common.GlobalTxHash = tx.Hash() // jhkim: set global variable

		msg, err := tx.AsMessage(types.MakeSigner(p.config, header.Number), header.BaseFee)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("could not apply tx %d [%v]: %w", i, tx.Hash().Hex(), err)
		}
		statedb.Prepare(tx.Hash(), i)
		receipt, err := applyTransaction(msg, p.config, p.bc, nil, gp, statedb, blockNumber, blockHash, tx, usedGas, vmenv)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("could not apply tx %d [%v]: %w", i, tx.Hash().Hex(), err)
		}
		receipts = append(receipts, receipt)
		allLogs = append(allLogs, receipt.Logs...)

		//jhkim: reset global variable after process each transaction
		common.GlobalTxTo = common.Address{}
		common.GlobalTxFrom = common.Address{}

	}
	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	p.engine.Finalize(p.bc, header, statedb, block.Transactions(), block.Uncles())
	common.BlockMinerList[blocknumber] = common.SimpleAccount{Addr: block.Coinbase(),
		Balance:     statedb.GetBalance(block.Coinbase()),
		Nonce:       statedb.GetNonce(block.Coinbase()),
		Codehash:    statedb.GetCodeHash(block.Coinbase()),
		StorageRoot: statedb.GetStorageTrieHash(block.Coinbase()),
	}
	uncles := block.Uncles()
	if len(uncles) != 0 {
		common.BlockUncleList[blocknumber] = []common.SimpleAccount{}
		for _, uncle := range uncles {
			tmp := common.SimpleAccount{Addr: uncle.Coinbase,
				Balance:     statedb.GetBalance(uncle.Coinbase),
				Nonce:       statedb.GetNonce(uncle.Coinbase),
				Codehash:    statedb.GetCodeHash(uncle.Coinbase),
				StorageRoot: statedb.GetStorageTrieHash(uncle.Coinbase),
			}
			common.BlockUncleList[blocknumber] = append(common.BlockUncleList[blocknumber], tmp)
		}

	}
	common.GlobalTxHash = common.Hash{}

	return receipts, allLogs, *usedGas, nil

}

//jhkim: check contract related tx
func isContractRelated(tx *types.Transaction, msg types.Message, statedb *state.StateDB) bool {
	// isContractRelated := false
	if tx.To() == nil { // contract deploy
		// isContractRelated = true
		return true
	} else {
		code := statedb.GetCode(*msg.To())
		if code == nil { // transfer
			return false
		} else { // contract call
			// isContractRelated = true

			if msg.Value() != big.NewInt(0) {
				// fmt.Println("dumbtx? transfer ether to contract")
				// fmt.Println("dumb tx/ blocknumber: ", common.GlobalBlockNumber, "txHash:", tx.Hash())
				// os.Exit(0)
			}
			return true
		}
	}

}

//jhkim: rlp encoding of common.SubStateAccount
func RLPEncodeSubstateAccount(sa common.SubstateAccount) []byte {
	var legacyAccount types.StateAccount

	legacyAccount.Balance = sa.Balance
	legacyAccount.CodeHash = sa.CodeHash
	legacyAccount.Nonce = sa.Nonce
	legacyAccount.Root = sa.StorageRoot

	data, _ := rlp.EncodeToBytes(legacyAccount)
	// fmt.Println("@@@@@@@RLPENCODEING@@@@@@@")
	// fmt.Println("LEGACY ACCOUNT", legacyAccount)
	// fmt.Println("nonce", legacyAccount.Nonce)
	// fmt.Println("balance", legacyAccount.Balance)
	// fmt.Println("root", legacyAccount.Root)
	// fmt.Println("codehash", legacyAccount.CodeHash)

	// fmt.Println("@@@@@@@")
	return data
}
func RLPEncodeSimpleAccount(sa common.SimpleAccount) []byte {
	var legacyAccount types.StateAccount
	legacyAccount.Balance = sa.Balance
	legacyAccount.CodeHash = sa.Codehash.Bytes()
	legacyAccount.Nonce = sa.Nonce
	legacyAccount.Root = sa.StorageRoot

	data, _ := rlp.EncodeToBytes(legacyAccount)
	// fmt.Println("@@@@@@@RLPENCODEING@@@@@@@")
	// fmt.Println("LEGACY ACCOUNT", legacyAccount)
	// fmt.Println("nonce", legacyAccount.Nonce)
	// fmt.Println("balance", legacyAccount.Balance)
	// fmt.Println("root", legacyAccount.Root)
	// fmt.Println("codehash", legacyAccount.CodeHash)

	// fmt.Println("@@@@@@@")
	return data
}

func applyTransaction(msg types.Message, config *params.ChainConfig, bc ChainContext, author *common.Address, gp *GasPool, statedb *state.StateDB, blockNumber *big.Int, blockHash common.Hash, tx *types.Transaction, usedGas *uint64, evm *vm.EVM) (*types.Receipt, error) {
	// Create a new context to be used in the EVM environment.
	txContext := NewEVMTxContext(msg)
	evm.Reset(txContext, statedb)
	common.GlobalTxHash = tx.Hash() // jhkim: set global variable
	// Apply the transaction to the current state (included in the env).
	result, err := ApplyMessage(evm, msg, gp)
	if err != nil {
		return nil, err
	}

	//jhkim: write Txdetail
	common.GlobalMutex.Lock()
	if _, ok := common.TxDetail[tx.Hash()]; !ok {
		WriteTxDetail(tx, msg, blockNumber, statedb) //jhkim
	} else {
		// fmt.Println("Error: Tx already exist!", tx.Hash(), common.GlobalBlockNumber)
		// os.Exit(0)
	}
	common.GlobalMutex.Unlock()

	if tx.To() == nil {
		common.GlobalDeployedContractAddress = crypto.CreateAddress(evm.TxContext.Origin, tx.Nonce()) // jhkim: for cacheing
	}

	// Update the state with pending changes.
	var root []byte
	if config.IsByzantium(blockNumber) {
		statedb.Finalise(true)
	} else {
		root = statedb.IntermediateRoot(config.IsEIP158(blockNumber)).Bytes()
	}
	*usedGas += result.UsedGas

	// Create a new receipt for the transaction, storing the intermediate root and gas used
	// by the tx.
	receipt := &types.Receipt{Type: tx.Type(), PostState: root, CumulativeGasUsed: *usedGas}
	if result.Failed() {
		receipt.Status = types.ReceiptStatusFailed

		// fmt.Println("Failed Transaction", common.GlobalBlockNumber, tx.Hash())
		// delete(common.TxDetail, tx.Hash())
		common.GlobalMutex.Lock()
		common.TxDetail[tx.Hash()].Types = 4 // marking failed transaction
		// fmt.Println("@#@#@#@# who is authour", author)
		// fmt.Println("@#@#@#@# before", common.TxWriteList[tx.Hash()])
		tmp := common.SubstateAlloc{}
		tmp[common.GlobalBlockMiner] = common.TxWriteList[tx.Hash()][common.GlobalBlockMiner]
		tmp[msg.From()] = common.TxWriteList[tx.Hash()][msg.From()]
		common.TxWriteList[tx.Hash()] = tmp
		// fmt.Println("@#@#@#@#", common.GlobalBlockMiner, msg.From())
		// fmt.Println("@#@#@#@#  after", common.TxWriteList[tx.Hash()])
		if result.Err == vm.ErrCodeStoreOutOfGas {
			// fmt.Println("Failed tx/ blocknumber:", common.GlobalBlockNumber, " txhash:", tx.Hash(), "result.err", result.Err) // 나중에 read list만 남기고 write list는 없애야함
			// fmt.Println("  Failed tx/ErrCodeStoreOutOfGas/block#:", common.GlobalBlockNumber, " txhash:", tx.Hash()) // 나중에 read list만 남기고 write list는 없애야함
			// fmt.Println()
		} else if result.Err == vm.ErrOutOfGas {
			// fmt.Println("  Failed tx/ErrOutOfGas/block#:", common.GlobalBlockNumber, " txhash:", tx.Hash()) // 나중에 read list만 남기고 write list는 없애야함
		}
		// tmp := common.TxSubstate[common.GlobalBlockNumber][tx.Hash()]
		// for k := range tmp {
		// 	if k != msg.From() && k != common.GlobalBlockMiner { // failed tx에서는 sender와 miner만 남기자
		// 		delete(tmp, k)
		// 		// tmp[k] = nil
		// 	}
		// }
		common.GlobalMutex.Unlock()
	} else {
		receipt.Status = types.ReceiptStatusSuccessful

	}

	// If the transaction created a contract, store the creation address in the receipt.
	if msg.To() == nil {
		receipt.ContractAddress = crypto.CreateAddress(evm.TxContext.Origin, tx.Nonce())
		// if common.GlobalBlockNumber == 54347 && receipt.ContractAddress != common.GlobalContractAddress {
		// 	fmt.Println("Caching deployed contract address is wrong")
		// }
		if receipt.Status == types.ReceiptStatusSuccessful {
			// common.GlobalMutex.Lock()
			common.TxDetail[tx.Hash()].DeployedContractAddress = receipt.ContractAddress
			// common.GlobalMutex.Unlock()
		}
	}
	receipt.TxHash = tx.Hash()
	receipt.GasUsed = result.UsedGas

	// Set the receipt logs and create the bloom filter.
	receipt.Logs = statedb.GetLogs(tx.Hash(), blockHash)
	receipt.Bloom = types.CreateBloom(types.Receipts{receipt})
	receipt.BlockHash = blockHash
	receipt.BlockNumber = blockNumber
	receipt.TransactionIndex = uint(statedb.TxIndex())
	return receipt, err
}

// ApplyTransaction attempts to apply a transaction to the given state database
// and uses the input parameters for its environment. It returns the receipt
// for the transaction, gas used and an error if the transaction failed,
// indicating the block was invalid.
func ApplyTransaction(config *params.ChainConfig, bc ChainContext, author *common.Address, gp *GasPool, statedb *state.StateDB, header *types.Header, tx *types.Transaction, usedGas *uint64, cfg vm.Config) (*types.Receipt, error) {
	msg, err := tx.AsMessage(types.MakeSigner(config, header.Number), header.BaseFee)
	if err != nil {
		return nil, err
	}
	// Create a new context to be used in the EVM environment
	blockContext := NewEVMBlockContext(header, bc, author)
	vmenv := vm.NewEVM(blockContext, vm.TxContext{}, statedb, config, cfg)
	return applyTransaction(msg, config, bc, author, gp, statedb, header.Number, header.Hash(), tx, usedGas, vmenv)
}

// jhkim: all transactions processed during syncing and mining are recorded
func WriteTxDetail(tx *types.Transaction, msg types.Message, number *big.Int, statedb *state.StateDB) {
	// common.GlobalMutex.Lock()
	// defer common.GlobalMutex.Unlock()
	// fmt.Println("WriteTxDetail", common.GlobalBlockNumber, tx.Hash())
	txInform := common.TxInformation{}
	// txInform.ContractAddress_SlotHash = map[common.Address]*[]common.Hash{}
	// txInform.BlockNumber = (*number).Int64()
	// txInform.Internal = []common.Address{}
	// txInform.InternalCA = []common.Address{}
	txInform.DeployedContractAddress = common.Address{}
	// txInform.AccountBalance = map[common.Address]uint64{}

	if tx.To() == nil {
		txInform.Types = 2 // contract creation

	} else {
		code := statedb.GetCode(*msg.To())

		if code == nil {
			txInform.Types = 1 // Transfer
		} else {
			txInform.Types = 3 // contract call
		}
	}

	// preimage of address hash
	txInform.From = msg.From()
	common.GlobalTxFrom = msg.From()
	if tx.To() != nil {
		txInform.To = *tx.To()
		common.GlobalTxTo = *tx.To()
	} else { // if contract creation tx
		txInform.To = common.HexToAddress("")
		common.GlobalTxTo = common.HexToAddress("")
	}
	common.TxDetail[tx.Hash()] = &txInform

}
