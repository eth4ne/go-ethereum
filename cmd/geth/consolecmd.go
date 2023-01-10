// Copyright 2016 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/console"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/rpc"
	"gopkg.in/urfave/cli.v1"
)

var (
	consoleFlags = []cli.Flag{utils.JSpathFlag, utils.ExecFlag, utils.PreloadJSFlag}

	consoleCommand = cli.Command{
		Action:   utils.MigrateFlags(localConsole),
		Name:     "console",
		Usage:    "Start an interactive JavaScript environment",
		Flags:    append(append(nodeFlags, rpcFlags...), consoleFlags...),
		Category: "CONSOLE COMMANDS",
		Description: `
The Geth console is an interactive shell for the JavaScript runtime environment
which exposes a node admin interface as well as the Ðapp JavaScript API.
See https://geth.ethereum.org/docs/interface/javascript-console.`,
	}

	attachCommand = cli.Command{
		Action:    utils.MigrateFlags(remoteConsole),
		Name:      "attach",
		Usage:     "Start an interactive JavaScript environment (connect to node)",
		ArgsUsage: "[endpoint]",
		Flags:     append(consoleFlags, utils.DataDirFlag),
		Category:  "CONSOLE COMMANDS",
		Description: `
The Geth console is an interactive shell for the JavaScript runtime environment
which exposes a node admin interface as well as the Ðapp JavaScript API.
See https://geth.ethereum.org/docs/interface/javascript-console.
This command allows to open a console on a running geth node.`,
	}

	javascriptCommand = cli.Command{
		Action:    utils.MigrateFlags(ephemeralConsole),
		Name:      "js",
		Usage:     "Execute the specified JavaScript files",
		ArgsUsage: "<jsfile> [jsfile...]",
		Flags:     append(nodeFlags, consoleFlags...),
		Category:  "CONSOLE COMMANDS",
		Description: `
The JavaScript VM exposes a node admin interface as well as the Ðapp
JavaScript API. See https://geth.ethereum.org/docs/interface/javascript-console`,
	}
	// jhkim: custom console command for replay transactions of specific blocks
	replayblockCommand = cli.Command{
		Action:    utils.MigrateFlags(replayBlockConsole),
		Name:      "replayblock",
		Usage:     "jhkim: Execute the transactions of given blocks",
		ArgsUsage: "<blocknumber>",
		Flags:     append(nodeFlags, consoleFlags...),
		Category:  "CONSOLE COMMANDS",
		Description: `
jhkim: Replay transactions of given number of block and make txsubstate text file of it`,
	}
)

// localConsole starts a new geth node, attaching a JavaScript console to it at the
// same time.
func localConsole(ctx *cli.Context) error {
	// Create and start the node based on the CLI flags
	prepare(ctx)
	stack, backend := makeFullNode(ctx)
	startNode(ctx, stack, backend, true)
	defer stack.Close()

	// Attach to the newly started node and create the JavaScript console.
	client, err := stack.Attach()
	if err != nil {
		return fmt.Errorf("Failed to attach to the inproc geth: %v", err)
	}
	config := console.Config{
		DataDir: utils.MakeDataDir(ctx),
		DocRoot: ctx.GlobalString(utils.JSpathFlag.Name),
		Client:  client,
		Preload: utils.MakeConsolePreloads(ctx),
	}
	console, err := console.New(config)
	if err != nil {
		return fmt.Errorf("Failed to start the JavaScript console: %v", err)
	}
	defer console.Stop(false)

	// If only a short execution was requested, evaluate and return.
	if script := ctx.GlobalString(utils.ExecFlag.Name); script != "" {
		console.Evaluate(script)
		return nil
	}

	// Track node shutdown and stop the console when it goes down.
	// This happens when SIGTERM is sent to the process.
	go func() {
		stack.Wait()
		console.StopInteractive()
	}()

	// Print the welcome screen and enter interactive mode.
	console.Welcome()
	console.Interactive()
	return nil
}

// remoteConsole will connect to a remote geth instance, attaching a JavaScript
// console to it.
func remoteConsole(ctx *cli.Context) error {
	endpoint := ctx.Args().First()
	if endpoint == "" {
		path := node.DefaultDataDir()
		if ctx.GlobalIsSet(utils.DataDirFlag.Name) {
			path = ctx.GlobalString(utils.DataDirFlag.Name)
		}
		if path != "" {
			if ctx.GlobalBool(utils.RopstenFlag.Name) {
				// Maintain compatibility with older Geth configurations storing the
				// Ropsten database in `testnet` instead of `ropsten`.
				legacyPath := filepath.Join(path, "testnet")
				if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
					path = legacyPath
				} else {
					path = filepath.Join(path, "ropsten")
				}
			} else if ctx.GlobalBool(utils.RinkebyFlag.Name) {
				path = filepath.Join(path, "rinkeby")
			} else if ctx.GlobalBool(utils.GoerliFlag.Name) {
				path = filepath.Join(path, "goerli")
			} else if ctx.GlobalBool(utils.SepoliaFlag.Name) {
				path = filepath.Join(path, "sepolia")
			}
		}
		endpoint = fmt.Sprintf("%s/geth.ipc", path)
	}
	client, err := dialRPC(endpoint)
	if err != nil {
		utils.Fatalf("Unable to attach to remote geth: %v", err)
	}
	config := console.Config{
		DataDir: utils.MakeDataDir(ctx),
		DocRoot: ctx.GlobalString(utils.JSpathFlag.Name),
		Client:  client,
		Preload: utils.MakeConsolePreloads(ctx),
	}
	console, err := console.New(config)
	if err != nil {
		utils.Fatalf("Failed to start the JavaScript console: %v", err)
	}
	defer console.Stop(false)

	if script := ctx.GlobalString(utils.ExecFlag.Name); script != "" {
		console.Evaluate(script)
		return nil
	}

	// Otherwise print the welcome screen and enter interactive mode
	console.Welcome()
	console.Interactive()
	return nil
}

// dialRPC returns a RPC client which connects to the given endpoint.
// The check for empty endpoint implements the defaulting logic
// for "geth attach" with no argument.
func dialRPC(endpoint string) (*rpc.Client, error) {
	if endpoint == "" {
		endpoint = node.DefaultIPCEndpoint(clientIdentifier)
	} else if strings.HasPrefix(endpoint, "rpc:") || strings.HasPrefix(endpoint, "ipc:") {
		// Backwards compatibility with geth < 1.5 which required
		// these prefixes.
		endpoint = endpoint[4:]
	}
	return rpc.Dial(endpoint)
}

// ephemeralConsole starts a new geth node, attaches an ephemeral JavaScript
// console to it, executes each of the files specified as arguments and tears
// everything down.
func ephemeralConsole(ctx *cli.Context) error {
	// Create and start the node based on the CLI flags
	stack, backend := makeFullNode(ctx)
	startNode(ctx, stack, backend, false)
	defer stack.Close()

	// Attach to the newly started node and start the JavaScript console
	client, err := stack.Attach()
	if err != nil {
		return fmt.Errorf("Failed to attach to the inproc geth: %v", err)
	}
	config := console.Config{
		DataDir: utils.MakeDataDir(ctx),
		DocRoot: ctx.GlobalString(utils.JSpathFlag.Name),
		Client:  client,
		Preload: utils.MakeConsolePreloads(ctx),
	}

	console, err := console.New(config)
	if err != nil {
		return fmt.Errorf("Failed to start the JavaScript console: %v", err)
	}
	defer console.Stop(false)

	// Interrupt the JS interpreter when node is stopped.
	go func() {
		stack.Wait()
		console.Stop(false)
	}()

	// Evaluate each of the specified JavaScript files.
	for _, file := range ctx.Args() {
		if err = console.Execute(file); err != nil {
			return fmt.Errorf("Failed to execute %s: %v", file, err)
		}
	}

	// The main script is now done, but keep running timers/callbacks.
	console.Stop(true)
	return nil
}

func replayBlockConsole(ctx *cli.Context) error {
	stack, _ := makeConfigNode(ctx)
	defer stack.Close()

	if ctx.NArg() > 2 {
		log.Error("Too many arguments given")
		return errors.New("too many arguments")
	}

	var (
		// 1 arguements
		blocknumber int

		// 2 arguements
		start int
		end   int

		argnum int
		err    error
	)
	if ctx.NArg() == 2 {
		argnum = 2
		start, err = strconv.Atoi(ctx.Args().Get(0))
		if err != nil {
			log.Error("Failed to resolve start blocknumber", "err", err)
			return err
		}
		end, err = strconv.Atoi(ctx.Args().Get(1))
		if err != nil {
			log.Error("Failed to resolve end blocknumber", "err", err)
			return err
		}
		if start > end {
			// log.Error("")
			return fmt.Errorf("START blocknumber %v should be bigger than END blocknumber %v", start, end)
		}
		log.Info(fmt.Sprintf("Replay transactions in block #%v ~ #%v", start, end))
	} else if ctx.NArg() == 1 {
		argnum = 1
		blocknumber, err = strconv.Atoi(ctx.Args().Get(0))
		if err != nil {
			log.Error("Failed to resolve blocknumber", "err", err)
			return err
		}
		log.Info(fmt.Sprintf("Replay transactions in block #%v", blocknumber))
	}

	chain, _ := utils.MakeChain(ctx, stack)
	if argnum == 1 {

		replayblock(blocknumber, chain)

		// for k, v := range common.TxDetail {
		// 	fmt.Println("  txhash:", k)
		// 	fmt.Println("    txDetail:")

		// 	fmt.Println("      From", v.From)
		// 	fmt.Println("      To", v.To)
		// 	fmt.Println("      Types", v.Types)
		// 	fmt.Println()
		// 	writelist := common.TxWriteList[k]
		// 	for kk, vv := range writelist {
		// 		fmt.Println("      address:", kk)
		// 		fmt.Println("        ", vv)
		// 	}
		// 	fmt.Println()
		// 	fmt.Println()
		// 	fmt.Println()
		// 	fmt.Println()
		// }

		core.PrintTxSubstate(blocknumber, 1, *chain.Config())
	} else if argnum == 2 {
		starttime := time.Now()
		if end-start > 1000 { // pretty replay for more than 1000 blocks
			for i := start; i < end; i += 1000 {

				replayblocks(i, i+999, chain)
				core.PrintTxSubstate(i+999, 1000, *chain.Config())
			}
		} else {
			replayblocks(start, end, chain)
			core.PrintTxSubstate(end, end-start+1, *chain.Config())
		}
		elapsedtime := time.Since(starttime)
		fmt.Println("Elapsedtime for replaying blocks:", elapsedtime.Seconds(), "seconds")
	} else {
		fmt.Println()
		fmt.Println("Wrong condition for input")
	}
	// core.PrintTxSubstate(blocknumber, 1, *chain.Config())
	return nil
}

func replayblock(blocknumber int, chain *core.BlockChain) {
	parent := chain.GetBlockByNumber(uint64(blocknumber - 1))
	block := chain.GetBlockByNumber(uint64(blocknumber))
	statedb, err := chain.StateAt(parent.Root())
	if err != nil {
		log.Error("Failed to get statedb of parent block", err)
	}
	// trie, err := statedb.Database().OpenTrie(parent.Root())
	// fmt.Println("statedb.Database().OpenTrie(parent.Root()).Hash(): ", trie.Hash())
	if err != nil {
		fmt.Println("Failed to open state trie", err)
	}

	var (
		txs    = block.Transactions()
		signer = types.MakeSigner(chain.Config(), block.Number())
		failed error
	)
	blockCtx := core.NewEVMBlockContext(block.Header(), chain, nil)

	if common.TxReadList == nil {
		common.TxReadList = make(map[common.Hash]map[common.Address]struct{})
		common.TxWriteList = make(map[common.Hash]map[common.Address]*common.SubstateAccount)
		common.BlockMinerList = make(map[int]common.SimpleAccount)
		common.BlockUncleList = make(map[int][]common.SimpleAccount)
		common.BlockTxList = make(map[int][]common.Hash)
		common.HardFork = make(map[common.Address]*common.SubstateAccount)

	}

	common.GlobalBlockNumber = int(block.Number().Int64()) //jhkim
	if common.BlockTxList[blocknumber] == nil {
		common.BlockTxList[blocknumber] = make([]common.Hash, 0)
	}
	common.TxDetail = make(map[common.Hash]*common.TxInformation)

	for i, tx := range txs {
		// fmt.Println("txhash", tx.Hash(), tx.To())
		// make txsubstate
		common.GlobalTxHash = tx.Hash()

		common.TxReadList[tx.Hash()] = map[common.Address]struct{}{}
		common.TxWriteList[tx.Hash()] = map[common.Address]*common.SubstateAccount{}
		common.BlockTxList[blocknumber] = append(common.BlockTxList[blocknumber], tx.Hash())

		msg, _ := tx.AsMessage(signer, block.BaseFee())
		core.WriteTxDetail(tx, msg, block.Number(), statedb)
		statedb.Prepare(tx.Hash(), i)
		vmenv := vm.NewEVM(blockCtx, core.NewEVMTxContext(msg), statedb, chain.Config(), vm.Config{})

		result, err := core.ApplyMessage(vmenv, msg, new(core.GasPool).AddGas(msg.Gas()))
		if err != nil {
			failed = err
			break
		}

		if result.Failed() {

			//jhkim: classify fail type (ex. transfer fail, contractcall fail, contract deploy fail)
			// t := common.TxDetail[tx.Hash()].Types
			if t := common.TxDetail[tx.Hash()].Types; t == 1 {
				common.TxDetail[tx.Hash()].Types = 41 // transfer fail
			} else if t == 2 {
				common.TxDetail[tx.Hash()].Types = 42 // contract deploy fail
			} else if t == 3 {
				common.TxDetail[tx.Hash()].Types = 43 // contract call fail
			} else {
				common.TxDetail[tx.Hash()].Types = 4 // ???? wrong tx?
			}

		}

		if msg.To() == nil {
			contractaddress := crypto.CreateAddress(msg.From(), tx.Nonce())
			common.TxDetail[tx.Hash()].DeployedContractAddress = contractaddress
			if result.Failed() { //jhkim
				if common.TxWriteList[tx.Hash()][contractaddress] == nil {
					common.TxWriteList[tx.Hash()][contractaddress] = common.NewSubstateAccount(0, common.Big0, common.Hex2Bytes("0x0"), statedb.GetStorageTrieHash(contractaddress))
					// fmt.Println("applyTransaction msg.To() == nil")
					common.PrettyTxWritePrint(tx.Hash(), contractaddress)
					if statedb.GetCode(contractaddress) != nil {
						common.TxWriteList[tx.Hash()][contractaddress].Code = statedb.GetCode(contractaddress)
					}
				}

			}
		}
		// Finalize the state so any modifications are written to the trie
		// Only delete empty objects if EIP158/161 (a.k.a Spurious Dragon) is in effect
		// statedb.Finalise(vmenv.ChainConfig().IsEIP158(block.Number()))
		statedb.IntermediateRoot(vmenv.ChainConfig().IsEIP158(block.Number()))
	}
	common.GlobalTxHash = common.HexToHash("0x0")

	// block miner와 uncle에게 reward가 들어가기 전의 상태만 불러올 수 있음
	// 따라서 편법을 사용함
	tmpstatedb, _ := chain.StateAt(block.Root())
	common.BlockMinerList[blocknumber] = common.SimpleAccount{
		Addr:        block.Coinbase(),
		Balance:     tmpstatedb.GetBalance(block.Coinbase()),
		Nonce:       tmpstatedb.GetNonce(block.Coinbase()),
		Codehash:    statedb.GetCodeHash(block.Coinbase()),
		StorageRoot: statedb.GetStorageTrieHash(block.Coinbase()),
	}

	for _, uncle := range block.Uncles() {
		tmp := common.SimpleAccount{
			Addr:        uncle.Coinbase,
			Balance:     tmpstatedb.GetBalance(uncle.Coinbase),
			Nonce:       tmpstatedb.GetNonce(uncle.Coinbase),
			Codehash:    statedb.GetCodeHash(uncle.Coinbase),
			StorageRoot: statedb.GetStorageTrieHash(uncle.Coinbase),
		}
		common.BlockUncleList[blocknumber] = append(common.BlockUncleList[blocknumber], tmp)
	}
	if failed != nil {
		fmt.Println("Failed to replay transactions/ block:", int(block.Number().Int64()), err)
		// return failed
	}

	// // Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	// // p.engine.Finalize(p.bc, header, statedb, block.Transactions(), block.Uncles())

	// accumulateRewards(chain.Config(), state, header, uncles)
	// header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))
}

func replayblocks(start, end int, chain *core.BlockChain) {
	if common.TxReadList == nil {
		common.TxReadList = make(map[common.Hash]map[common.Address]struct{})
		common.TxWriteList = make(map[common.Hash]map[common.Address]*common.SubstateAccount)
		common.BlockMinerList = make(map[int]common.SimpleAccount)
		common.BlockUncleList = make(map[int][]common.SimpleAccount)
		common.BlockTxList = make(map[int][]common.Hash)
		common.HardFork = make(map[common.Address]*common.SubstateAccount)

	}

	common.TxDetail = make(map[common.Hash]*common.TxInformation)

	for blocknumber := start; blocknumber < end+1; blocknumber++ {
		parent := chain.GetBlockByNumber(uint64(blocknumber - 1))
		block := chain.GetBlockByNumber(uint64(blocknumber))

		statedb, err := chain.StateAt(parent.Root())
		if err != nil {
			log.Error("Failed to get statedb of parent block", err)
		}

		blockCtx := core.NewEVMBlockContext(block.Header(), chain, nil)

		var (
			txs    = block.Transactions()
			signer = types.MakeSigner(chain.Config(), block.Number())
			failed error
		)

		common.GlobalBlockNumber = int(block.Number().Int64()) //jhkim
		if common.BlockTxList[blocknumber] == nil {
			common.BlockTxList[blocknumber] = make([]common.Hash, 0)
		}

		for i, tx := range txs {
			// fmt.Println("block#", blocknumber, "txhash", tx.Hash(), tx.To())
			// make txsubstate
			common.GlobalTxHash = tx.Hash()

			common.TxReadList[tx.Hash()] = map[common.Address]struct{}{}
			common.TxWriteList[tx.Hash()] = map[common.Address]*common.SubstateAccount{}
			common.BlockTxList[blocknumber] = append(common.BlockTxList[blocknumber], tx.Hash())

			msg, _ := tx.AsMessage(signer, block.BaseFee())

			statedb.Prepare(tx.Hash(), i)
			vmenv := vm.NewEVM(blockCtx, core.NewEVMTxContext(msg), statedb, chain.Config(), vm.Config{})

			result, err := core.ApplyMessage(vmenv, msg, new(core.GasPool).AddGas(msg.Gas()))
			core.WriteTxDetail(tx, msg, block.Number(), statedb)
			if err != nil {
				failed = err
				break
			}

			if result.Failed() {

				//jhkim: classify fail type (ex. transfer fail, contractcall fail, contract deploy fail)
				// t := common.TxDetail[tx.Hash()].Types
				if t := common.TxDetail[tx.Hash()].Types; t == 1 {
					common.TxDetail[tx.Hash()].Types = 41 // transfer fail
				} else if t == 2 {
					common.TxDetail[tx.Hash()].Types = 42 // contract deploy fail
				} else if t == 3 {
					common.TxDetail[tx.Hash()].Types = 43 // contract call fail
				} else {
					common.TxDetail[tx.Hash()].Types = 4 // ???? wrong tx?
				}

			}

			if msg.To() == nil {
				contractaddress := crypto.CreateAddress(msg.From(), tx.Nonce())
				common.TxDetail[tx.Hash()].DeployedContractAddress = contractaddress
				if result.Failed() { //jhkim
					if common.TxWriteList[tx.Hash()][contractaddress] == nil {
						common.TxWriteList[tx.Hash()][contractaddress] = common.NewSubstateAccount(0, common.Big0, common.Hex2Bytes("0x0"), statedb.GetStorageTrieHash(contractaddress))
						// fmt.Println("applyTransaction msg.To() == nil")
						common.PrettyTxWritePrint(tx.Hash(), contractaddress)
						if statedb.GetCode(contractaddress) != nil {
							common.TxWriteList[tx.Hash()][contractaddress].Code = statedb.GetCode(contractaddress)
						}
					}

				}
			}
			// Finalize the state so any modifications are written to the trie
			// Only delete empty objects if EIP158/161 (a.k.a Spurious Dragon) is in effect
			statedb.Finalise(vmenv.ChainConfig().IsEIP158(block.Number()))

			statedb.IntermediateRoot(vmenv.ChainConfig().IsEIP158(block.Number()))
			common.GlobalTxHash = common.HexToHash("0x0")
		}

		// block miner와 uncle에게 reward가 들어가기 전의 상태만 불러올 수 있음
		// 따라서 편법을 사용함
		tmpstatedb, _ := chain.StateAt(block.Root())
		common.BlockMinerList[blocknumber] = common.SimpleAccount{
			Addr:        block.Coinbase(),
			Balance:     tmpstatedb.GetBalance(block.Coinbase()),
			Nonce:       tmpstatedb.GetNonce(block.Coinbase()),
			Codehash:    statedb.GetCodeHash(block.Coinbase()),
			StorageRoot: statedb.GetStorageTrieHash(block.Coinbase()),
		}

		for _, uncle := range block.Uncles() {
			tmp := common.SimpleAccount{
				Addr:        uncle.Coinbase,
				Balance:     tmpstatedb.GetBalance(uncle.Coinbase),
				Nonce:       tmpstatedb.GetNonce(uncle.Coinbase),
				Codehash:    statedb.GetCodeHash(uncle.Coinbase),
				StorageRoot: statedb.GetStorageTrieHash(uncle.Coinbase),
			}
			common.BlockUncleList[blocknumber] = append(common.BlockUncleList[blocknumber], tmp)
		}
		if failed != nil {
			fmt.Println("Failed to replay transactions/ block:", int(block.Number().Int64()), err)
			// return failed
		}

	}

}
