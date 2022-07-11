package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/big"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/fdlimit"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/leveldb"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

const (
	// port for requests
	serverPort = "8999"

	// maximum byte length of response
	maxResponseLen = 4096

	// simulation result log file path
	logFilePath = "./logFiles/"

	// trie graph json file path
	trieGraphPath = "./trieGraphs/"

	// choose leveldb vs memorydb
	useLeveldb = true
	// leveldb path
	leveldbPath = "/home/jmlee/ssd/mptSimulator/trieNodes"
	// leveldb cache size (MB) (Geth default: 512) (memory leak might occur when calling reset() frequently with too big cache size)
	leveldbCache = 1024
	// leveldb options
	leveldbNamespace = "eth/db/chaindata/"
	leveldbReadonly  = false
)

var (
	// # of max open files for leveldb (Geth default: 524288)
	leveldbHandles = 524288

	// empty account
	emptyAccount types.StateAccount
	// emptyCodeHash for EoA accounts
	emptyCodeHash = crypto.Keccak256(nil)
	// emptyRoot is the known root hash of an empty trie.
	emptyRoot = common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")

	// disk to store trie nodes (either leveldb or memorydb)
	diskdb ethdb.KeyValueStore
	// normal state trie
	normTrie *trie.Trie
	// secure state trie
	secureTrie *trie.SecureTrie

	// same as SecureTrie.hashKeyBuf (to mimic SecureTrie)
	hashKeyBuf = make([]byte, common.HashLength)
)

func reset() {

	// set maximum number of open files
	limit, err := fdlimit.Maximum()
	if err != nil {
		// Fatalf("Failed to retrieve file descriptor allowance: %v", err)
		fmt.Println("Failed to retrieve file descriptor allowance:", err)
	}
	raised, err := fdlimit.Raise(uint64(limit))
	if err != nil {
		// Fatalf("Failed to raise file descriptor allowance: %v", err)
		fmt.Println("Failed to raise file descriptor allowance:", err)
	}
	if raised <= 1000 {
		fmt.Println("max open file num is too low")
		os.Exit(1)
	}
	leveldbHandles = int(raised / 2)
	fmt.Println("open file limit:", limit, "/ raised:", raised, "/ leveldbHandles:", leveldbHandles)

	// reset normal trie
	if diskdb != nil {
		diskdb.Close()
	}
	if useLeveldb {
		fmt.Println("set leveldb")
		// if do not delete directory, this just reopens existing db
		err := os.RemoveAll(leveldbPath)
		if err != nil {
			fmt.Println("RemoveAll error ! ->", err)
		}

		diskdb, err = leveldb.New(leveldbPath, leveldbCache, leveldbHandles, leveldbNamespace, leveldbReadonly)
		if err != nil {
			fmt.Println("leveldb.New error!! ->", err)
			os.Exit(1)
		}
	} else {
		fmt.Println("set memorydb")
		diskdb = memorydb.New()
	}
	normTrie, _ = trie.New(common.Hash{}, trie.NewDatabase(diskdb))

	// reset secure trie
	secureTrie, _ = trie.NewSecure(common.Hash{}, trie.NewDatabase(memorydb.New()))

	// reset db stats
	common.TotalNodeStat.Reset()
	common.NewNodeStat.Reset()

	common.TrieNodeInfos = make(map[common.Hash]common.NodeInfo)
	common.TrieNodeInfosDirty = make(map[common.Hash]common.NodeInfo)

	// reset account
	emptyAccount.Balance = big.NewInt(0)
	emptyAccount.Nonce = 0
	emptyAccount.CodeHash = emptyCodeHash
	emptyAccount.Root = emptyRoot

	// reset blocks
	common.Blocks = make(map[uint64]common.BlockInfo)
	common.CurrentBlockNum = 0

	// set genesis block (with empty state trie)
	flushTrieNodes()
	// tricky codes to correctly set genesis block and current block number
	common.Blocks[0] = common.Blocks[1]
	delete(common.Blocks, 1)
	common.CurrentBlockNum = 0
}

// rollbackUncommittedUpdates goes back to latest flushed trie (i.e., undo all uncommitted trie updates)
// discards unflushed dirty nodes
func rollbackUncommittedUpdates() {
	// open current state trie
	currentRoot := common.Blocks[common.CurrentBlockNum].Root
	normTrie, _ = trie.New(currentRoot, trie.NewDatabase(diskdb))

	// rollback account nonce value
	emptyAccount.Nonce = common.Blocks[common.CurrentBlockNum].MaxAccountNonce

	// drop all dirty nodes
	common.TrieNodeInfosDirty = make(map[common.Hash]common.NodeInfo)
}

// rollback goes back to the target block state
func rollbackToBlock(targetBlockNum uint64) bool {

	// check rollback validity
	if targetBlockNum >= common.CurrentBlockNum {
		fmt.Println("cannot rollback to current or future")
		return false
	}
	if _, exist := common.Blocks[targetBlockNum]; !exist {
		fmt.Println("cannot rollback to target block: target block info is deleted (rollback limit)")
		return false
	}
	if targetBlockNum == 0 {
		// just execute reset(), when we rollback to block 0
		reset()
		return true
	}

	// rollback db modifications
	fmt.Println("rollbackToBlock: from block", common.CurrentBlockNum, "to block", targetBlockNum)
	for blockNum := common.CurrentBlockNum; blockNum > targetBlockNum; blockNum-- {
		blockInfo, exist := common.Blocks[blockNum]
		if !exist {
			fmt.Println("rollbackToBlock error")
			os.Exit(1)
		}

		// delete flushed nodes
		for _, nodeHash := range blockInfo.FlushedNodeHashes {
			// delete from disk
			err := diskdb.Delete(nodeHash.Bytes())
			if err != nil {
				fmt.Println("diskdb.Delete error:", err)
			}

			// get node info
			nodeInfo, _ := common.TrieNodeInfos[nodeHash]
			nodeSize := uint64(nodeInfo.Size)

			// update db stats
			if nodeInfo.IsLeafNode {
				common.TotalNodeStat.LeafNodesNum--
				common.TotalNodeStat.LeafNodesSize -= nodeSize
			} else if nodeInfo.IsShortNode {
				common.TotalNodeStat.ShortNodesNum--
				common.TotalNodeStat.ShortNodesSize -= nodeSize
			} else {
				common.TotalNodeStat.FullNodesNum--
				common.TotalNodeStat.FullNodesSize -= nodeSize
			}

			// delete from TrieNodeInfos
			delete(common.TrieNodeInfos, nodeHash)
		}

		// delete block
		delete(common.Blocks, blockNum)
		common.CurrentBlockNum--
	}

	// open target block's trie
	normTrie, _ = trie.New(common.Blocks[targetBlockNum].Root, trie.NewDatabase(diskdb))

	// set nonce value at that time
	emptyAccount.Nonce = common.Blocks[targetBlockNum].MaxAccountNonce

	// reset new trie nodes info
	common.NewNodeStat.Reset()

	// print db stats
	totalNodesNum, totalNodesSize := common.TotalNodeStat.GetSum()
	fmt.Println("  rollback finished -> total trie nodes:", totalNodesNum, "/ db size:", totalNodesSize)

	return true
}

// estimate storage increment when flush this trie
func estimateIncrement(hash common.Hash) (uint64, uint64) {

	if nodeInfo, exist := common.TrieNodeInfosDirty[hash]; exist {
		incNum := uint64(0)
		incAmount := uint64(0)
		for _, childHash := range nodeInfo.ChildHashes {
			num, amount := estimateIncrement(childHash)
			incNum += num
			incAmount += amount
		}
		return incNum + 1, incAmount + uint64(nodeInfo.Size)
	} else {
		// this node is not dirty node
		return 0, 0
	}
}

// flushTrieNodes flushes dirty trie nodes to the db
func flushTrieNodes() {
	// increase block number
	common.CurrentBlockNum++

	// reset db stats before flush
	common.NewNodeStat.Reset()

	// flush trie nodes
	normTrie.Commit(nil)
	normTrie.TrieDB().Commit(normTrie.Hash(), false, nil)

	// update block info (to be able to rollback to this state later)
	// fmt.Println("new trie root after flush:", normTrie.Hash().Hex())
	blockInfo, _ := common.Blocks[common.CurrentBlockNum]
	blockInfo.Root = normTrie.Hash()
	blockInfo.MaxAccountNonce = emptyAccount.Nonce
	common.Blocks[common.CurrentBlockNum] = blockInfo
	// delete too old block to store
	blockNumToDelete := common.CurrentBlockNum - common.MaxBlocksToStore - 1
	if _, exist := common.Blocks[blockNumToDelete]; exist {
		// fmt.Println("current block num:", common.CurrentBlockNum, "/ block num to delete:", blockNumToDelete)
		delete(common.Blocks, blockNumToDelete)
	}

	// discard unflushed dirty nodes after flush
	common.TrieNodeInfosDirty = make(map[common.Hash]common.NodeInfo)

	// show db stat
	totalNodesNum, totalNodesSize := common.TotalNodeStat.GetSum()
	newNodesNum, newNodesSize := common.NewNodeStat.GetSum()
	common.NewNodeStat.Print()
	fmt.Println("  new trie nodes:", newNodesNum, "/ increased db size:", newNodesSize)
	fmt.Println("  total trie nodes:", totalNodesNum, "/ db size:", totalNodesSize)
}

func countOpenFiles() int64 {
	out, err := exec.Command("/bin/sh", "-c", fmt.Sprintf("lsof -p %v", os.Getpid())).Output()
	if err != nil {
		fmt.Println(err.Error())
	}
	lines := strings.Split(string(out), "\n")
	return int64(len(lines) - 1)
}

func randomHex(n int) string {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return ""
	}
	return hex.EncodeToString(bytes)
}

func generateRandomAddress() common.Address {
	randHex := randomHex(20)
	randAddr := common.HexToAddress(randHex)
	fmt.Println("rand addr:", randAddr.Hex())
	return randAddr
}

// hashKey returns the hash of key as an ephemeral buffer
func hashKey(key []byte) []byte {
	h := trie.NewHasher(false)
	h.Sha().Reset()
	h.Sha().Write(key)
	h.Sha().Read(hashKeyBuf[:])
	trie.HasherPool.Put(h)
	return hashKeyBuf[:]
}

// (deprecated: storing db stats in common/types.go)
// inspectDB iterates db and returns # of total trie nodes and their size
func inspectDB(diskdb ethdb.KeyValueStore) (uint64, uint64) {
	// iterate db
	it := diskdb.NewIterator(nil, nil)
	totalNodes := uint64(0)
	totalSize := common.StorageSize(0)
	for it.Next() {
		var (
			key  = it.Key()
			size = common.StorageSize(len(key) + len(it.Value()))
		)
		// fmt.Println("node hash:", hex.EncodeToString(key), "/ value:", it.Value(), "/ size: ", size)
		// fmt.Println("node hash:", hex.EncodeToString(key), "/ size: ", size)
		totalNodes++
		totalSize += size
	}
	fmt.Println("total nodes:", totalNodes, "/ total size:", totalSize)
	return totalNodes, uint64(totalSize)
}

// inspectTrie measures number and size of trie nodes in the trie
// (count duplicated nodes)
func inspectTrie(hash common.Hash) {
	// fmt.Println("insepctTrie() node hash:", hash.Hex())

	// find this node's info
	nodeInfo, isFlushedNode := common.TrieNodeInfos[hash]
	if !isFlushedNode {
		var exist bool
		nodeInfo, exist = common.TrieNodeInfosDirty[hash]
		if !exist {
			// this node is unknown, just return
			// fmt.Println("insepctTrie() unknown node")
			return
		}
	}

	// dynamic programming approach
	totalNum, _ := nodeInfo.SubTrieNodeStat.GetSum()
	if totalNum != 0 {
		// stop measuring
		// fmt.Println("insepctTrie() stop measuring")
		return
	}

	// measure trie node num & size
	subTrieFullNodesNum := uint64(0)
	subTrieShortNodesNum := uint64(0)
	subTrieLeafNodesNum := uint64(0)

	subTrieFullNodesSize := uint64(0)
	subTrieShortNodesSize := uint64(0)
	subTrieLeafNodesSize := uint64(0)

	for _, childHash := range nodeInfo.ChildHashes {
		// inspect child node
		inspectTrie(childHash)

		// get child node info
		childNodeInfo, exist := common.TrieNodeInfos[childHash]
		if !exist {
			childNodeInfo, exist = common.TrieNodeInfosDirty[childHash]
			if !exist {
				fmt.Println("error: inspectTrie() -> child node do not exist")
				os.Exit(1)
			}
		}

		// collect nums
		subTrieFullNodesNum += childNodeInfo.SubTrieNodeStat.FullNodesNum
		subTrieShortNodesNum += childNodeInfo.SubTrieNodeStat.ShortNodesNum
		subTrieLeafNodesNum += childNodeInfo.SubTrieNodeStat.LeafNodesNum

		// collect sizes
		subTrieFullNodesSize += childNodeInfo.SubTrieNodeStat.FullNodesSize
		subTrieShortNodesSize += childNodeInfo.SubTrieNodeStat.ShortNodesSize
		subTrieLeafNodesSize += childNodeInfo.SubTrieNodeStat.LeafNodesSize
	}

	// measure itself
	nodeSize := uint64(nodeInfo.Size)
	if nodeInfo.IsLeafNode {
		subTrieLeafNodesNum++
		subTrieLeafNodesSize += nodeSize
	} else if nodeInfo.IsShortNode {
		subTrieShortNodesNum++
		subTrieShortNodesSize += nodeSize
	} else {
		subTrieFullNodesNum++
		subTrieFullNodesSize += nodeSize
	}

	// memorize node stat
	nodeInfo.SubTrieNodeStat.FullNodesNum = subTrieFullNodesNum
	nodeInfo.SubTrieNodeStat.ShortNodesNum = subTrieShortNodesNum
	nodeInfo.SubTrieNodeStat.LeafNodesNum = subTrieLeafNodesNum
	nodeInfo.SubTrieNodeStat.FullNodesSize = subTrieFullNodesSize
	nodeInfo.SubTrieNodeStat.ShortNodesSize = subTrieShortNodesSize
	nodeInfo.SubTrieNodeStat.LeafNodesSize = subTrieLeafNodesSize
	if isFlushedNode {
		common.TrieNodeInfos[hash] = nodeInfo
	} else {
		common.TrieNodeInfosDirty[hash] = nodeInfo
	}
}

// trieToGraph converts trie to graph (edges & features)
func trieToGraph(hash common.Hash) {

	// find this node's info
	nodeInfo, isFlushedNode := common.TrieNodeInfos[hash]
	if !isFlushedNode {
		var exist bool
		nodeInfo, exist = common.TrieNodeInfosDirty[hash]
		if !exist {
			// this node is unknown, just return
			// fmt.Println("insepctTrieDP() unknown node")
			return
		}
	}

	// set features
	if nodeInfo.IsLeafNode {
		common.TrieGraph.Features[hash.Hex()] = "0"
	} else if nodeInfo.IsShortNode {
		common.TrieGraph.Features[hash.Hex()] = "1"
	} else {
		common.TrieGraph.Features[hash.Hex()] = "2"
	}

	// set edges
	for i, childHash := range nodeInfo.ChildHashes {
		_ = i
		// fmt.Println("  inspect children -> i:", i, "/ childHash:", childHash.Hex())

		// get child node info (just for debugging)
		_, exist := common.TrieNodeInfos[childHash]
		if !exist {
			_, exist = common.TrieNodeInfosDirty[childHash]
			if !exist {
				fmt.Println("error: inspectTrie() -> child node do not exist")
				os.Exit(1)
			}
		}

		// add edges
		common.TrieGraph.Edges = append(common.TrieGraph.Edges, []string{hash.Hex(), childHash.Hex()})

		// recursive call
		trieToGraph(childHash)
	}

}

}

func updateTrie(key []byte) error {
	// encoding value
	emptyAccount.Nonce++ // to make different leaf node
	data, _ := rlp.EncodeToBytes(emptyAccount)

	// update state trie
	// fmt.Println("update trie -> key:", common.BytesToHash(key).Hex(), "/ nonce:", emptyAccount.Nonce)
	err := normTrie.TryUpdate(key, data)
	return err
}

func ConnHandler(conn net.Conn) {
	recvBuf := make([]byte, 4096)
	for {
		// wait for message from client
		n, err := conn.Read(recvBuf)
		if err != nil {
			if err == io.EOF {
				log.Println(err)
				return
			}
			log.Println(err)
			return
		}

		// deal with request
		if 0 < n {
			// read message from client
			data := recvBuf[:n]
			request := string(data)
			// fmt.Println("message from client:", request)

			//
			// do something with the request
			//
			response := make([]byte, maxResponseLen)
			params := strings.Split(request, ",")
			// fmt.Println("params:", params)
			switch params[0] {

			case "reset":
				fmt.Println("execute reset()")
				reset()
				response = []byte("reset success")

			case "getBlockNum":
				fmt.Println("execute getBlockNum()")
				response = []byte(strconv.FormatUint(common.CurrentBlockNum, 10))

			case "getTrieRootHash":
				fmt.Println("execute getTrieRootHash()")
				rootHash := normTrie.Hash().Hex()
				fmt.Println("current trie root hash:", rootHash)
				response = []byte(rootHash)

			case "printCurrentTrie":
				fmt.Println("execute printCurrentTrie()")
				normTrie.Hash()
				normTrie.Print()
				response = []byte("success")

			case "inspectDB":
				fmt.Println("execute inspectDB()")
				totalNodesNum, totalNodesSize := common.TotalNodeStat.GetSum()
				fmt.Println("print inspectDB result")
				common.TotalNodeStat.Print()

				response = []byte(strconv.FormatUint(totalNodesNum, 10) + "," + strconv.FormatUint(totalNodesSize, 10))

			case "inspectTrie":
				inspectDB(diskdb)
				fmt.Println("execute inspectTrie()")
				rootHash := normTrie.Hash()
				fmt.Println("root hash:", rootHash.Hex())
				inspectTrie(rootHash)
				nodeInfo, exist := common.TrieNodeInfos[rootHash]
				if !exist {
					nodeInfo, exist = common.TrieNodeInfosDirty[rootHash]
				}
				fmt.Println("print inspectTrie result")
				nodeInfo.SubTrieNodeStat.Print()

				response = []byte(rootHash.Hex() + "," + nodeInfo.SubTrieNodeStat.ToString(","))

			case "inspectSubTrie":
				inspectDB(diskdb)
				fmt.Println("execute inspectSubTrie()")
				rootHash := common.HexToHash(params[1])
				fmt.Println("rootHash:", rootHash.Hex())
				inspectTrie(rootHash)
				nodeInfo, exist := common.TrieNodeInfos[rootHash]
				if !exist {
					nodeInfo, exist = common.TrieNodeInfosDirty[rootHash]
				}
				fmt.Println("print inspectSubTrie result")
				nodeInfo.SubTrieNodeStat.Print()

				response = []byte(rootHash.Hex() + "," + nodeInfo.SubTrieNodeStat.ToString(","))

			case "flush":
				fmt.Println("execute flushTrieNodes()")
				flushTrieNodes()
				newNodesNum, newNodesSize := common.NewNodeStat.GetSum()
				response = []byte(strconv.FormatUint(newNodesNum, 10) + "," + strconv.FormatUint(newNodesSize, 10))

			case "updateTrie":
				key, err := hex.DecodeString(params[1]) // convert hex string to bytes
				if err != nil {
					fmt.Println("ERROR: failed decoding")
					response = []byte("ERROR: fail decoding while updateTrie")
				} else {
					// fmt.Println("execute updateTrie() -> key:", common.Bytes2Hex(key))
					err = updateTrie(key)
					if err == nil {
						// fmt.Println("success updateTrie -> new root hash:", normTrie.Hash().Hex())
						response = []byte("success updateTrie")
					} else {
						// this is not normal case, need to fix certain error
						fmt.Println("ERROR: fail updateTrie:", err, "/ key:", params[1])
						os.Exit(1)
					}
				}
				normTrie.Hash()
				// normTrie.Print()
				// fmt.Println("\n\n")

			case "rollbackUncommittedUpdates":
				fmt.Println("execute rollbackUncommittedUpdates()")
				rollbackUncommittedUpdates()
				response = []byte("success rollbackUncommittedUpdates")

			case "rollbackToBlock":
				targetBlockNum, err := strconv.ParseUint(params[1], 10, 64)
				fmt.Println("rollback target block number:", targetBlockNum)
				if err != nil {
					fmt.Println("ParseUint error:", err)
					os.Exit(1)
				}
				doSuccess := rollbackToBlock(targetBlockNum)
				if doSuccess {
					response = []byte("success rollbackToBlock")
				} else {
					response = []byte("fail rollbackToBlock")
				}

			case "estimateFlushResult":
				fmt.Println("execute estimateFlushResult()")

				// inspect latest flushed trie
				flushedRoot := common.Blocks[common.CurrentBlockNum].Root
				inspectTrie(flushedRoot)
				flushedRootInfo, existF := common.TrieNodeInfos[flushedRoot]
				if !existF {
					flushedRootInfo, existF = common.TrieNodeInfosDirty[flushedRoot]
				}
				flushedNodesNum, flushedNodesSize := flushedRootInfo.SubTrieNodeStat.GetSum()

				// inspect current trie
				currentRoot := normTrie.Hash()
				inspectTrie(currentRoot)
				currentRootInfo, existC := common.TrieNodeInfos[currentRoot]
				if !existC {
					currentRootInfo, existC = common.TrieNodeInfosDirty[currentRoot]
				}
				currentNodesNum, currentNodesSize := currentRootInfo.SubTrieNodeStat.GetSum()

				// estimate flush result
				incTrieNum := currentNodesNum - flushedNodesNum
				incTrieSize := currentNodesSize - flushedNodesSize
				incTotalNodesNum, incTotalNodesSize := estimateIncrement(currentRoot)

				response = []byte(strconv.FormatUint(incTrieNum, 10) +
					"," + strconv.FormatUint(incTrieSize, 10) +
					"," + strconv.FormatUint(incTotalNodesNum, 10) +
					"," + strconv.FormatUint(incTotalNodesSize, 10))

			case "trieToGraph":
				fmt.Println("execute trieToGraph()")

				// reset trie graph
				common.TrieGraph = common.TrieGraphInfo{}
				common.TrieGraph.Features = make(map[string]string)

				// convert trie to graph
				root := normTrie.Hash()
				trieToGraph(root)

				// create log file directory if not exist
				if _, err := os.Stat(trieGraphPath); errors.Is(err, os.ErrNotExist) {
					err := os.Mkdir(trieGraphPath, os.ModePerm)
					if err != nil {
						log.Println(err)
					}
				}

				// write graph as a json file
				graph, err := json.MarshalIndent(common.TrieGraph, "", " ")
				if err != nil {
					fmt.Println("json.MarshalIndent() error:", err)
					os.Exit(1)
				}

				// save log to the file
				err = ioutil.WriteFile(trieGraphPath+root.Hex()+".json", graph, 0644)
				if err != nil {
					fmt.Println("WriteFile() error:", err)
					os.Exit(1)
				}

				// return json file name (at trieGraphPath/hash.json)
				response = []byte(root.Hex())

			case "printAllStats":
				// save this as a file (param: file name)
				logFileName := params[1]
				_ = logFileName
				fmt.Println("execute printAllStats()")

				delimiter := " "

				rootHash := normTrie.Hash()
				inspectTrie(rootHash)
				nodeInfo, exist := common.TrieNodeInfos[rootHash]
				if !exist {
					nodeInfo, exist = common.TrieNodeInfosDirty[rootHash]
				}

				totalResult := common.TotalNodeStat.ToString(delimiter)
				subTrieResult := nodeInfo.SubTrieNodeStat.ToString(delimiter)

				fmt.Print(totalResult)
				fmt.Print(subTrieResult)

				// create log file directory if not exist
				if _, err := os.Stat(logFilePath); errors.Is(err, os.ErrNotExist) {
					err := os.Mkdir(logFilePath, os.ModePerm)
					if err != nil {
						log.Println(err)
					}
				}

				// save log to the file
				logFile, err := os.OpenFile(logFilePath+logFileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if err != nil {
					fmt.Println("ERR", "err", err)
				}
				logData := totalResult + subTrieResult
				fmt.Fprintln(logFile, logData)
				logFile.Close()

				response = []byte("success")

			default:
				fmt.Println("ERROR: there is no matching request")
				response = []byte("ERROR: there is no matching request")
			}

			// send response to client
			_, err = conn.Write(response[:])
			// fmt.Println("")
			if err != nil {
				log.Println(err)
				return
			}
		}
	}
}

// makeTestTrie generates a trie for test/debugging
func makeTestTrie() {

	normTrie, _ = trie.New(common.Hash{}, trie.NewDatabase(diskdb))
	accountsNum := 8
	trieCommitEpoch := 4
	emptyAccount.Nonce = 0
	emptyAccount.CodeHash = emptyCodeHash
	emptyAccount.Root = emptyRoot
	for i := 0; i < accountsNum; i++ {
		// make incremental hex
		randHex := fmt.Sprintf("%x", i) // make int as hex string
		//fmt.Println("address hex string:", randHex)

		randAddr := common.HexToAddress(randHex)
		// fmt.Println("insert account addr:", randAddr.Hex())
		// fmt.Println("insert account addr:", randAddr[:])
		// fmt.Println("addrHash:", hex.EncodeToString(hashKey(randAddr[:])))
		// fmt.Println("addrHash:", hashKey(randAddr[:]))

		// encoding value
		emptyAccount.Nonce++ // to make different leaf node
		data, _ := rlp.EncodeToBytes(emptyAccount)

		// insert account into trie
		// 1. as a normal trie
		normTrie.TryUpdate(randAddr[:], data)
		// 2. as a secure trie
		// hk := hashKey(randAddr[:])
		// normTrie.TryUpdate(hk, data)

		if (i+1)%trieCommitEpoch == 0 {
			flushTrieNodes()
		}
		// fmt.Println("state", i+1)
		// normTrie.Print()
		// fmt.Println("\n\n")
	}

}

func main() {

	// initialize
	reset()

	// for test, run this function
	// makeTestTrie()

	// open tcp socket
	fmt.Println("open socket")
	listener, err := net.Listen("tcp", ":"+serverPort)
	fmt.Println(listener.Addr())
	if nil != err {
		log.Println(err)
	}
	defer listener.Close()

	// wait for requests
	for {
		fmt.Println("wait for requests...")
		conn, err := listener.Accept()
		if nil != err {
			log.Println(err)
			continue
		}
		defer conn.Close()

		// go ConnHandler(conn) // asynchronous
		ConnHandler(conn) // synchronous
	}

}
