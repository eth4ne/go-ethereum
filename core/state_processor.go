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
	"os"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
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
	}
	blockContext := NewEVMBlockContext(header, p.bc, nil)
	vmenv := vm.NewEVM(blockContext, vm.TxContext{}, statedb, p.config, cfg)

	common.GlobalBlockNumber = int(block.Number().Int64()) //jhkim
	common.GlobalBlockMiner = block.Coinbase()
	uncles := block.Uncles()
	for _, uncle := range uncles {
		common.GlobalBlockUncles = append(common.GlobalBlockUncles, uncle.Coinbase)
	}

	// if common.GlobalBlockNumber == 80631 {
	// 	fmt.Println("block 80631", common.GlobalBlockMiner, common.GlobalBlockUncles)
	// 	이거 의미없다. transaction이 수행되면서 miner랑 uncle이 결정될텐데 지금 count해봐야 의미가 없다
	// 	근데 왜 잘찍히는지 모르겠다
	// }
	// fmt.Println("set GlobalBlockNumber", common.GlobalBlockNumber)

	// Iterate over and process the individual transactions
	// if common.GlobalBlockNumber == 198695 {
	// 	fmt.Println("stateProcessor.go Process")
	// }

	for i, tx := range block.Transactions() {
		// if common.GlobalBlockNumber == 55284 {
		// 	fmt.Println("before applyTransaction", tx.Hash())
		// }
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
		// if common.GlobalBlockNumber == 46402 {
		// 	fmt.Println("46402 Did transactions")
		// }
		receipts = append(receipts, receipt)
		allLogs = append(allLogs, receipt.Logs...)

		//jhkim: reset global variable after process each transaction
		// common.GlobalTxHash = common.HexToHash("0x0")

		common.GlobalTxTo = common.Address{}
		common.GlobalTxFrom = common.Address{}
	}
	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	p.engine.Finalize(p.bc, header, statedb, block.Transactions(), block.Uncles())

	return receipts, allLogs, *usedGas, nil
}

func applyTransaction(msg types.Message, config *params.ChainConfig, bc ChainContext, author *common.Address, gp *GasPool, statedb *state.StateDB, blockNumber *big.Int, blockHash common.Hash, tx *types.Transaction, usedGas *uint64, evm *vm.EVM) (*types.Receipt, error) {
	// Create a new context to be used in the EVM environment.
	txContext := NewEVMTxContext(msg)
	evm.Reset(txContext, statedb)
	// fmt.Println("applytransaction", common.GlobalBlockNumber, tx.Hash())
	// Apply the transaction to the current state (included in the env).
	result, err := ApplyMessage(evm, msg, gp)
	if err != nil {
		return nil, err
	}

	common.GlobalMutex.Lock()

	if _, ok := common.TxDetail[tx.Hash()]; !ok {
		// if common.GlobalBlockNumber == 55284 {
		// 	fmt.Println("WriteTxDetail", tx.Hash())
		// }
		WriteTxDetail(tx, msg, blockNumber, statedb) //jhkim
	} else {
		fmt.Println("Error: Tx already exist!", tx.Hash(), common.GlobalBlockNumber)
		os.Exit(0)
	}
	common.GlobalMutex.Unlock()
	if tx.To() == nil {
		common.GlobalContractAddress = crypto.CreateAddress(evm.TxContext.Origin, tx.Nonce()) // jhkim: for cacheing
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
			common.GlobalMutex.Lock()
			common.TxDetail[tx.Hash()].DeployedContractAddress = receipt.ContractAddress
			common.GlobalMutex.Unlock()
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
	txInform.ContractAddress_SlotHash = map[common.Address]*[]common.Hash{}
	txInform.BlockNumber = (*number).Int64()
	txInform.Else = []common.Address{}
	txInform.DeployedContractAddress = common.Address{}

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
