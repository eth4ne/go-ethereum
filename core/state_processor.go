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

	// jhkim: nil map check
	if common.TxReadList == nil {
		common.TxReadList = make(map[common.Hash]map[common.Address]struct{})
		common.TxWriteList = make(map[common.Hash]map[common.Address]*common.SubstateAccount)
		common.BlockMinerList = make(map[int]common.SimpleAccount)
		common.BlockUncleList = make(map[int][]common.SimpleAccount)
		common.BlockTxList = make(map[int][]common.Hash)
		common.HardFork = make(map[common.Address]*common.SubstateAccount)

	}

	// jhkim: Init common.Global varables for ethane before transactions processing
	common.GlobalBlockNumber = int(block.Number().Int64()) //jhkim
	blocknumber := common.GlobalBlockNumber
	if common.BlockTxList[blocknumber] == nil {
		common.BlockTxList[blocknumber] = make([]common.Hash, 0)
	}

	// jhkim: write block miner & uncles
	minerStorageRoot := statedb.GetStorageTrieHash(block.Coinbase())
	if minerStorageRoot != common.EmptyRoot {
		fmt.Println("Block", common.GlobalBlockNumber, "'s miner", block.Coinbase(), " has not empty storage root/ storageroot:", minerStorageRoot)
		// os.Exit(0)
	}

	uncles := block.Uncles()
	if len(uncles) != 0 {
		common.BlockUncleList[blocknumber] = []common.SimpleAccount{}
		for _, uncle := range uncles {
			uncleStorageRoot := statedb.GetStorageTrieHash(uncle.Coinbase)
			if uncleStorageRoot != common.EmptyRoot {
				fmt.Println("Block", common.GlobalBlockNumber, "'s uncle", uncle.Coinbase, "has not empty storage root/ storageroot:", uncleStorageRoot)
				// os.Exit(0)
			}
		}

	}

	// Iterate over and process the individual transactions
	for i, tx := range block.Transactions() {

		common.TxReadList[tx.Hash()] = map[common.Address]struct{}{}
		common.TxWriteList[tx.Hash()] = map[common.Address]*common.SubstateAccount{}
		common.BlockTxList[blocknumber] = append(common.BlockTxList[blocknumber], tx.Hash())
		common.GlobalTxHash = tx.Hash()

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

		//jhkim: reset global variables after process each transaction
		common.GlobalTxTo = common.Address{}
		common.GlobalTxFrom = common.Address{}
		common.GlobalTxHash = common.HexToHash("0x0")

	}
	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	p.engine.Finalize(p.bc, header, statedb, block.Transactions(), block.Uncles())

	common.BlockMinerList[blocknumber] = common.SimpleAccount{
		Addr:        block.Coinbase(),
		Balance:     statedb.GetBalance(block.Coinbase()),
		Nonce:       statedb.GetNonce(block.Coinbase()),
		Codehash:    statedb.GetCodeHash(block.Coinbase()),
		StorageRoot: minerStorageRoot, // jhkim: should be emptyhash
	}

	for _, uncle := range uncles {
		uncleStorageRoot := statedb.GetStorageTrieHash(block.Coinbase())
		tmp := common.SimpleAccount{
			Addr:        uncle.Coinbase,
			Balance:     statedb.GetBalance(uncle.Coinbase),
			Nonce:       statedb.GetNonce(uncle.Coinbase),
			Codehash:    statedb.GetCodeHash(uncle.Coinbase),
			StorageRoot: uncleStorageRoot, // jhkim: should be emptyhash
		}
		common.BlockUncleList[blocknumber] = append(common.BlockUncleList[blocknumber], tmp)
	}

	return receipts, allLogs, *usedGas, nil
}

func applyTransaction(msg types.Message, config *params.ChainConfig, bc ChainContext, author *common.Address, gp *GasPool, statedb *state.StateDB, blockNumber *big.Int, blockHash common.Hash, tx *types.Transaction, usedGas *uint64, evm *vm.EVM) (*types.Receipt, error) {
	// Create a new context to be used in the EVM environment.
	txContext := NewEVMTxContext(msg)
	evm.Reset(txContext, statedb)

	// jhkim: write TxDetail befor applymessage for test
	if _, ok := common.TxDetail[tx.Hash()]; !ok {
		WriteTxDetail(tx, msg, blockNumber, statedb)
	} else {
		fmt.Println("Error: Tx already exist!/ blocknumber", common.GlobalBlockNumber, " txHash:", tx.Hash())
		os.Exit(0)
	}

	// Apply the transaction to the current state (included in the env).
	result, err := ApplyMessage(evm, msg, gp)
	if err != nil {
		return nil, err
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

		// jhkim: ErrCodeStoreOutofGas is error message for only after Homestead.
		// So before Homestead(1150000), txDetail should not mark this tx as failed
		if !config.IsHomestead(blockNumber) && result.Err == vm.ErrCodeStoreOutOfGas {
			// jhkim: Do nothing
		} else {

			//jhkim: classify fail type (ex. transfer fail, contractcall fail, contract deploy fail)
			t := common.TxDetail[tx.Hash()].Types
			if t == 1 {
				common.TxDetail[tx.Hash()].Types = 41 // transfer fail
			} else if t == 2 {
				common.TxDetail[tx.Hash()].Types = 42 // contract deploy fail
			} else if t == 3 {
				common.TxDetail[tx.Hash()].Types = 43 // contract call fail
			} else {
				common.TxDetail[tx.Hash()].Types = 4 // ???? wrong tx?
			}

			// jhkim: Every failed transaction should have only 2 write list. miner and sender
			// We have 2 corner cases to conider. First miner is sender, and second miner is receiver
			// If miner is sender and this tx failed, writelist has only 1 item, miner
			// If miner is receiver and this tx failed, writelist has 2 items, miner and sender <- same as default
			// ** some dumb miners accept 0 fee transactions like 0x98970bfb525897505fa0c6b32ea306554761138319d34e01b19691f438557a09,
			// so "Every failed transaction should have only 2 write list. miner and sender" is wrong

			// tmp := common.SubstateAlloc{}
			// tmp := map[common.Address]*common.SubstateAccount{}

			// tmp[common.GlobalBlockMiner] = common.TxWriteList[tx.Hash()][common.GlobalBlockMiner]
			// tmp[msg.From()] = common.TxWriteList[tx.Hash()][msg.From()]
			// common.TxWriteList[tx.Hash()] = tmp
			// fmt.Println("Failed txwritelist after ", tx.Hash(),common.TxWriteList[tx.Hash()])

		}

		// jhkim: Debugging
		// if result.Err == vm.ErrCodeStoreOutOfGas {
		// 	fmt.Println("  Failed tx/ErrCodeStoreOutOfGas/block#:", common.GlobalBlockNumber, " txhash:", tx.Hash())
		// } else if result.Err == vm.ErrOutOfGas {
		// 	fmt.Println("  Failed tx/ErrOutOfGas/block#:", common.GlobalBlockNumber, " txhash:", tx.Hash())
		// } else {
		// 	fmt.Println("  Failed tx/ELSE/block#:", common.GlobalBlockNumber, " txhash:", tx.Hash(), result.Err)
		// }

	} else {
		receipt.Status = types.ReceiptStatusSuccessful
	}
	receipt.TxHash = tx.Hash()
	receipt.GasUsed = result.UsedGas

	// If the transaction created a contract, store the creation address in the receipt.
	if msg.To() == nil {
		receipt.ContractAddress = crypto.CreateAddress(evm.TxContext.Origin, tx.Nonce())
		common.TxDetail[tx.Hash()].DeployedContractAddress = receipt.ContractAddress
		if receipt.Status == types.ReceiptStatusSuccessful { //jhkim
			if common.TxWriteList[tx.Hash()][receipt.ContractAddress] == nil {
				common.TxWriteList[tx.Hash()][receipt.ContractAddress] = common.NewSubstateAccount(0, common.Big0, common.Hex2Bytes("0x0"), statedb.GetStorageTrieHash(receipt.ContractAddress))
				// fmt.Println("applyTransaction msg.To() == nil")
				common.PrettyTxWritePrint(tx.Hash(), receipt.ContractAddress)
				if statedb.GetCode(receipt.ContractAddress) != nil {
					common.TxWriteList[tx.Hash()][receipt.ContractAddress].Code = statedb.GetCode(receipt.ContractAddress)
				}
			}

		}
	}

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
	txInform.InternalDeployedAddress = make([]common.Address, 0)
	txInform.DeletedAddress = make([]common.Address, 0)
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
