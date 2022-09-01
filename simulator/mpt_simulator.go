package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/big"
	mrand "math/rand"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

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
	// empty ethane account
	emptyEthaneAccount types.EthaneStateAccount
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
	// storage tries which have unflushed trie updates
	// dirtyStorageTries[contractAddr] = opened storage trie
	// (reset dirtyStorageTries after flush)
	dirtyStorageTries = make(map[common.Address]*trie.SecureTrie)

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
	common.TotalStorageNodeStat.Reset()
	common.NewNodeStat.Reset()
	common.NewStorageNodeStat.Reset()

	common.TrieNodeInfos = make(map[common.Hash]common.NodeInfo)
	common.TrieNodeInfosDirty = make(map[common.Hash]common.NodeInfo)

	// reset account
	emptyAccount.Balance = big.NewInt(0)
	emptyAccount.Nonce = 0
	emptyAccount.CodeHash = emptyCodeHash
	emptyAccount.Root = emptyRoot

	// reset ethane account
	emptyEthaneAccount.Balance = big.NewInt(0)
	emptyEthaneAccount.Nonce = 0
	emptyEthaneAccount.CodeHash = emptyCodeHash
	emptyEthaneAccount.Root = emptyRoot

	// reset blocks
	common.Blocks = make(map[uint64]common.BlockInfo)
	common.CurrentBlockNum = 0

	// reset Ethane related vars
	common.AddrToKeyActive = make(map[common.Address]common.Hash)
	common.AddrToKeyInactive = make(map[common.Address][]common.Hash)
	common.NextKey = uint64(0)
	common.CheckpointKey = uint64(0)
	common.KeysToDelete = make([]common.Hash, 0)
	common.InactiveBoundaryKey = uint64(0)
	common.CheckpointKeys = make(map[uint64]uint64)
	common.RestoredKeys = make([]common.Hash, 0)

	// set genesis block (with empty state trie)
	flushTrieNodes()
	// tricky codes to correctly set genesis block and current block number
	common.Blocks[0] = common.Blocks[1]
	delete(common.Blocks, 1)
	common.CurrentBlockNum = 0
	common.CheckpointKeys[0] = common.CheckpointKey
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

		// TODO(jmlee): split FlushedNodeHashes -> FlushedNodeHashes + FlushedStorageNodeHashes
		// for _, nodeHash := range blockInfo.FlushedStorageNodeHashes {
		// 	// delete from disk
		// 	err := diskdb.Delete(nodeHash.Bytes())
		// 	if err != nil {
		// 		fmt.Println("diskdb.Delete error:", err)
		// 	}

		// 	// get node info
		// 	nodeInfo, _ := common.TrieNodeInfos[nodeHash]
		// 	nodeSize := uint64(nodeInfo.Size)

		// 	// update db stats
		// 	if nodeInfo.IsLeafNode {
		// 		common.TotalStorageNodeStat.LeafNodesNum--
		// 		common.TotalStorageNodeStat.LeafNodesSize -= nodeSize
		// 	} else if nodeInfo.IsShortNode {
		// 		common.TotalStorageNodeStat.ShortNodesNum--
		// 		common.TotalStorageNodeStat.ShortNodesSize -= nodeSize
		// 	} else {
		// 		common.TotalStorageNodeStat.FullNodesNum--
		// 		common.TotalStorageNodeStat.FullNodesSize -= nodeSize
		// 	}

		// 	// delete from TrieNodeInfos
		// 	delete(common.TrieNodeInfos, nodeHash)
		// }

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
	common.NewStorageNodeStat.Reset()

	// print db stats
	totalNodesNum, totalNodesSize := common.TotalNodeStat.GetSum()
	totalStorageNodesNum, totalStorageNodesSize := common.TotalStorageNodeStat.GetSum()
	fmt.Println("  rollback finished -> total trie nodes:", totalNodesNum+totalStorageNodesNum, "/ db size:", totalNodesSize+totalStorageNodesSize)

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

	fmt.Println("flush: generate block number", common.CurrentBlockNum)

	// do inactivate or delete previous leaf nodes
	if common.IsEthane {

		// update checkpoint key
		bn := common.CurrentBlockNum
		common.CheckpointKeys[bn] = common.CheckpointKey
		common.CheckpointKey = common.NextKey

		// delete
		if (bn+1)%common.DeleteEpoch == 0 {
			deletePrevLeafNodes()
		}

		// inactivate
		if (bn+1)%common.InactivateEpoch == 0 && bn != common.InactivateEpoch-1 {
			// check condition (lastBlockNum >= 0)
			if bn >= common.InactivateCriterion {
				// find block range to inactivate (firstBlockNum ~ lastBlockNum)
				firstBlockNum := uint64(0)
				if bn+1 >= common.InactivateCriterion+common.InactivateEpoch {
					firstBlockNum = bn - common.InactivateCriterion - common.InactivateEpoch + 1
				}
				lastBlockNum := bn - common.InactivateCriterion

				// find keys that have leaf nodes within inactivate range in trie
				firstKey := common.Uint64ToHash(common.CheckpointKeys[firstBlockNum])
				lastKey := common.Uint64ToHash(common.CheckpointKeys[lastBlockNum+1] - 1)

				// fmt.Println("first block num:", firstBlockNum)
				// fmt.Println("last block num:", lastBlockNum)
				// fmt.Println("firstKey:", firstKey.Big().Int64())
				// fmt.Println("lastKey:", lastKey.Big().Int64())

				if firstKey.Big().Int64() <= lastKey.Big().Int64() {
					// inactivate old accounts
					inactivateLeafNodes(firstKey, lastKey)

					// delete used merkle proofs for restoration
					// TODO(jmlee): need independent epoch?
					deleteRestoredLeafNodes()
				}

			}
		}

		// store inactive boundary key in this block
		common.InactiveBoundaryKeys[bn] = common.InactiveBoundaryKey
	}

	// reset db stats before flush
	common.NewNodeStat.Reset()
	common.NewStorageNodeStat.Reset()

	// flush state trie nodes
	common.FlushStorageTries = false
	normTrie.Commit(nil)
	normTrie.TrieDB().Commit(normTrie.Hash(), false, nil)

	// flush storage trie nodes
	common.FlushStorageTries = true
	for _, storageTrie := range dirtyStorageTries {
		storageTrie.Commit(nil)
		storageTrie.TrieDB().Commit(storageTrie.Hash(), false, nil)
	}
	// reset dirty storage tries after flush
	dirtyStorageTries = make(map[common.Address]*trie.SecureTrie)

	// update block info (to be able to rollback to this state later)
	// fmt.Println("new trie root after flush:", normTrie.Hash().Hex())
	blockInfo, _ := common.Blocks[common.CurrentBlockNum]
	blockInfo.Root = normTrie.Hash()
	blockInfo.MaxAccountNonce = emptyAccount.Nonce
	blockInfo.NewNodeStat = common.NewNodeStat
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
	totalStorageNodesNum, totalStorageNodesSize := common.TotalStorageNodeStat.GetSum()
	newNodesNum, newNodesSize := common.NewNodeStat.GetSum()
	newStorageNodesNum, newStorageNodesSize := common.NewStorageNodeStat.GetSum()
	common.NewNodeStat.Print()
	fmt.Println("  new trie nodes:", newNodesNum, "/ increased db size:", newNodesSize)
	fmt.Println("  new storage trie nodes:", newStorageNodesNum, "/ increased db size:", newStorageNodesSize)
	fmt.Println("  total trie nodes:", totalNodesNum, "/ db size:", totalNodesSize)
	fmt.Println("  total storage trie nodes:", totalStorageNodesNum, "/ db size:", totalStorageNodesSize)
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

// inspectTrieWithinRange measures number and size of trie nodes in the sub trie
// (count duplicated nodes)
// do not utilize dynamic programming, just keep searching until it reaches to leaf nodes
func inspectTrieWithinRange(hash common.Hash, depth uint, leafNodeDepths *[]uint, prefix string, startKey, endKey []byte) common.NodeStat {
	// fmt.Println("insepctTrie() node hash:", hash.Hex())

	// find this node's info
	nodeInfo, isFlushedNode := common.TrieNodeInfos[hash]
	if !isFlushedNode {
		var exist bool
		nodeInfo, exist = common.TrieNodeInfosDirty[hash]
		if !exist {
			// this node is unknown, just return
			// fmt.Println("insepctTrie() unknown node")
			return common.NodeStat{}
		}
	}

	// check range
	prefix = prefix + nodeInfo.Key // fullNode's key = "", so does not matter
	prefixBytes := trie.StringKeybytesToHex(prefix)
	prefixBytesLen := len(prefixBytes)
	subStartKey := startKey[:prefixBytesLen]
	subEndKey := endKey[:prefixBytesLen]
	if bytes.Compare(subStartKey, prefixBytes) > 0 || bytes.Compare(subEndKey, prefixBytes) < 0 {
		// out of range, stop inspecting
		return common.NodeStat{}
	}

	// measure trie nodes num & size
	subTrieFullNodesNum := uint64(0)
	subTrieShortNodesNum := uint64(0)
	subTrieLeafNodesNum := uint64(0)

	subTrieFullNodesSize := uint64(0)
	subTrieShortNodesSize := uint64(0)
	subTrieLeafNodesSize := uint64(0)

	for i, childHash := range nodeInfo.ChildHashes {
		// inspect child node
		var childNodeStat common.NodeStat
		if len(nodeInfo.Indices) != 0 {
			// this node is full node
			childNodeStat = inspectTrieWithinRange(childHash, depth+1, leafNodeDepths, prefix+nodeInfo.Indices[i], startKey, endKey)
		} else {
			// this node is short node
			childNodeStat = inspectTrieWithinRange(childHash, depth+1, leafNodeDepths, prefix, startKey, endKey)
		}

		// collect nums
		subTrieFullNodesNum += childNodeStat.FullNodesNum
		subTrieShortNodesNum += childNodeStat.ShortNodesNum
		subTrieLeafNodesNum += childNodeStat.LeafNodesNum

		// collect sizes
		subTrieFullNodesSize += childNodeStat.FullNodesSize
		subTrieShortNodesSize += childNodeStat.ShortNodesSize
		subTrieLeafNodesSize += childNodeStat.LeafNodesSize
	}

	// measure itself
	nodeSize := uint64(nodeInfo.Size)
	if nodeInfo.IsLeafNode {
		subTrieLeafNodesNum++
		subTrieLeafNodesSize += nodeSize

		*leafNodeDepths = append(*leafNodeDepths, depth)
	} else if nodeInfo.IsShortNode {
		subTrieShortNodesNum++
		subTrieShortNodesSize += nodeSize
	} else {
		subTrieFullNodesNum++
		subTrieFullNodesSize += nodeSize
	}

	// return result
	var nodeStat common.NodeStat
	nodeStat.FullNodesNum = subTrieFullNodesNum
	nodeStat.ShortNodesNum = subTrieShortNodesNum
	nodeStat.LeafNodesNum = subTrieLeafNodesNum
	nodeStat.FullNodesSize = subTrieFullNodesSize
	nodeStat.ShortNodesSize = subTrieShortNodesSize
	nodeStat.LeafNodesSize = subTrieLeafNodesSize
	return nodeStat
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

	//
	// collect features: (nodeType,nodeSize,childHashes)
	//
	delimiter := ","
	common.TrieGraph.Features[hash.Hex()] = make(map[string]string)

	// add node type
	if nodeInfo.IsLeafNode {
		common.TrieGraph.Features[hash.Hex()]["type"] = "0"
	} else if nodeInfo.IsShortNode {
		common.TrieGraph.Features[hash.Hex()]["type"] = "1"
	} else {
		common.TrieGraph.Features[hash.Hex()]["type"] = "2"
	}

	// add node size
	common.TrieGraph.Features[hash.Hex()]["size"] = strconv.FormatUint(uint64(nodeInfo.Size), 10)

	// add edges & childHashes
	for _, childHash := range nodeInfo.ChildHashes {

		// add edges
		common.TrieGraph.Edges = append(common.TrieGraph.Edges, []string{hash.Hex(), childHash.Hex()})

		// add child hashes
		common.TrieGraph.Features[hash.Hex()]["childHashes"] += childHash.Hex()
		common.TrieGraph.Features[hash.Hex()]["childHashes"] += delimiter

		// recursive call
		trieToGraph(childHash)
	}
	// delete last delimiter
	childHashesLen := len(common.TrieGraph.Features[hash.Hex()]["childHashes"])
	if childHashesLen != 0 {
		common.TrieGraph.Features[hash.Hex()]["childHashes"] = common.TrieGraph.Features[hash.Hex()]["childHashes"][:childHashesLen-1]
	} else {
		common.TrieGraph.Features[hash.Hex()]["childHashes"] = ""
	}

	// TODO(jmlee): add prefix
	// common.TrieGraph.Features[hash.Hex()] += delimiter
	// common.TrieGraph.Features[hash.Hex()]["prefix"] += nodeInfo.Prefix

	// TODO(jmlee): add depth
	// common.TrieGraph.Features[hash.Hex()] += delimiter
	// common.TrieGraph.Features[hash.Hex()]["depth"] += nodeInfo.Depth

}

// find shortest prefixes among short nodes in the trie
func findShortestPrefixAmongShortNodes(hash common.Hash) ([]string, bool) {
	// fmt.Println("@ node hash:", hash.Hex())
	// check that the node exist
	nodeInfo, exist := common.TrieNodeInfosDirty[hash]
	if !exist {
		nodeInfo, exist = common.TrieNodeInfos[hash]
		if !exist {
			// fmt.Println("  this node do not exist")
			return []string{""}, false
		}
	}

	// find shortest prefix
	if nodeInfo.IsShortNode || nodeInfo.IsLeafNode {
		// this node is short node
		// fmt.Println("  this node is short node")
		return []string{""}, true
	} else {
		// this node is full node
		shortestPrefixLen := 10000 // in secure trie, 64+1 is enough
		shortestPrefixes := []string{}
		findPrefix := false
		for i, childHash := range nodeInfo.ChildHashes {
			subPrefixes, success := findShortestPrefixAmongShortNodes(childHash)
			for j, subPrefix := range subPrefixes {
				subPrefixes[j] = nodeInfo.Indices[i] + subPrefix
			}

			if success {
				findPrefix = true
				if len(subPrefixes[0]) == shortestPrefixLen {
					for _, subPrefix := range subPrefixes {
						shortestPrefixes = append(shortestPrefixes, subPrefix)
					}
				} else if len(subPrefixes[0]) < shortestPrefixLen {
					shortestPrefixLen = len(subPrefixes[0])
					shortestPrefixes = []string{}
					for _, subPrefix := range subPrefixes {
						shortestPrefixes = append(shortestPrefixes, subPrefix)
					}
				}
			}
		}
		if findPrefix {
			return shortestPrefixes, true
		}
	}

	// there is no short node in this trie
	// fmt.Println("  there is no short node in this trie")
	return []string{""}, false
}

func updateTrie(key, value []byte) error {
	// update state trie
	err := normTrie.TryUpdate(key, value)
	return err
}

func updateStorageTrie(storageTrie *trie.SecureTrie, key, value common.Hash) error {

	var v []byte
	if (value == common.Hash{}) {
		storageTrie.TryDelete(key[:])
	} else {
		// Encoding []byte cannot fail, ok to ignore the error.
		v, _ = rlp.EncodeToBytes(common.TrimLeftZeroes(value[:]))
		// fmt.Println("slot:", key, "/ slotValue:", v)
		storageTrie.TryUpdate(key[:], v)
	}

	// TODO(jmlee): return error
	return nil
}

//
// functions for Ethane
//

// update state trie following Ethane protocol
func updateTrieForEthane(addr common.Address, rlpedAccount []byte) error {

	// get addrKey of this address
	addrKey, exist := common.AddrToKeyActive[addr]
	if !exist {
		// this is new address or crumb account
		// fmt.Println("this is new address or crumb account")
		addrKey = common.HexToHash(strconv.FormatUint(common.NextKey, 16))
		// fmt.Println("make new account -> addr:", addr.Hex(), "/ keyHash:", newAddrKey)
		common.AddrToKeyActive[addr] = addrKey
		common.NextKey++
	}
	addrKeyUint := common.HashToUint64(addrKey)
	// fmt.Println("addr:", addr.Hex(), "/ addrKey:", addrKey.Big().Int64())

	// update state trie
	if addrKeyUint >= common.CheckpointKey {
		// newly created address or already moved address in this block
		// do not change its addrKey, just update
		// fmt.Println("insert -> key:", addrKey.Hex(), "/ addr:", addr.Hex())
		if err := normTrie.TryUpdate(addrKey[:], rlpedAccount); err != nil {
			fmt.Println("ERROR: updateTrieForEthane() failed to update trie 1 -> addr:", addr.Hex(), "/ addrKey:", addrKey)
			os.Exit(1)
		}

	} else if addrKeyUint >= common.InactiveBoundaryKey {
		// this address is already in the trie, so move the previous leaf node to the right side
		// but do not delete now, this useless previous leaf node will be deleted at every delete epoch
		common.KeysToDelete = append(common.KeysToDelete, addrKey)
		// fmt.Println("add KeysToDelete:", addrKey.Big().Int64())

		// insert new leaf node at next key position
		newAddrHash := common.HexToHash(strconv.FormatUint(common.NextKey, 16))
		common.AddrToKeyActive[addr] = newAddrHash
		// fmt.Println("insert -> key:", newAddrHash.Hex(), "/ addr:", addr.Hex())
		if err := normTrie.TryUpdate(newAddrHash[:], rlpedAccount); err != nil {
			fmt.Println("ERROR: updateTrieForEthane() failed to update trie 2 -> addr:", addr[:], "/ addrKey:", addrKey)
			os.Exit(1)
		}
		// fmt.Println("move leaf node to right -> addr:", addr.Hex(), "/ keyHash:", newAddrHash)

		// increase next key for future trie updates
		common.NextKey++

	} else {
		// addrKeyUint < common.InactiveBoundaryKey
		// this is impossible, should not reach to here
		fmt.Println("ERROR: addrKeyUint < common.InactiveBoundaryKey -> this is impossible, should not reach to here")
		os.Exit(1)
	}

	// no error occured (os.Exit(1) is not called)
	return nil
}

// delete previous leaf nodes in active trie
func deletePrevLeafNodes() {
	fmt.Println("delete prev leaf nodes() executed")

	// delete all keys in trie
	for _, keyToDelete := range common.KeysToDelete {
		// fmt.Println("  key to delete:", keyToDelete.Big().Int64())
		err := normTrie.TryDelete(keyToDelete[:])
		if err != nil {
			fmt.Println("ERROR deletePrevLeafNodes(): trie.TryDelete ->", err)
			os.Exit(1)
		}
	}

	// init keys to delete
	common.KeysToDelete = make([]common.Hash, 0)
}

// delete already used merkle proofs in inactive trie
func deleteRestoredLeafNodes() {

	// delete all used merkle proofs for restoration
	for _, keyToDelete := range common.RestoredKeys {
		err := normTrie.TryDelete(keyToDelete[:])
		if err != nil {
			fmt.Println("ERROR deleteRestoredLeafNodes(): trie.TryDelete ->", err)
			os.Exit(1)
		}
	}

	// init keys to delete
	common.RestoredKeys = make([]common.Hash, 0)
}

// move inactive accounts from active trie to inactive trie
func inactivateLeafNodes(firstKeyToCheck, lastKeyToCheck common.Hash) {
	// fmt.Println("do inactivation -> firstKey:", firstKeyToCheck.Big().Int64(), "lastKey:", lastKeyToCheck.Big().Int64())

	// find accounts to inactivate (i.e., all leaf nodes in the range after delete prev leaf nodes)
	accountsToInactivate, keysToInactivate, _ := normTrie.FindLeafNodes(firstKeyToCheck[:], lastKeyToCheck[:])

	// move inactive leaf nodes to left
	for index, key := range keysToInactivate {
		addr := common.BytesToAddress(accountsToInactivate[index]) // BytesToAddress() turns last 20 bytes into addr

		// delete inactive account from right
		// fmt.Println("(Inactivate)delete -> key:", key.Hex())
		err := normTrie.TryUpdate(key[:], nil)
		if err != nil {
			fmt.Println("inactivateLeafNodes delete error:", err)
			os.Exit(1)
		}

		// insert inactive account to left
		keyToInsert := common.Uint64ToHash(common.InactiveBoundaryKey)
		common.InactiveBoundaryKey++
		// fmt.Println("(Inactivate)insert -> key:", keyToInsert.Hex())
		err = normTrie.TryUpdate(keyToInsert[:], accountsToInactivate[index])
		if err != nil {
			fmt.Println("inactivateLeafNodes insert error:", err)
			os.Exit(1)
		}

		// update AddrToKey (active & inactive)
		delete(common.AddrToKeyActive, addr)
		common.AddrToKeyInactive[addr] = append(common.AddrToKeyInactive[addr], keyToInsert)
	}
}

// restore this address's accounts
func restoreAccount(restoreAddr common.Address) {

	// record merkle proofs for restoration
	// RESTORE_ALL: restore all inactive leaf nodes of this address
	common.RestoredKeys = append(common.RestoredKeys, common.AddrToKeyInactive[restoreAddr]...)
	delete(common.AddrToKeyInactive, restoreAddr)

	// TODO(jmlee): reconstruct real latest state later
	// temply now, just raise state update with meaningless data
	emptyEthaneAccount.Nonce++
	emptyEthaneAccount.Addr = restoreAddr
	data, _ := rlp.EncodeToBytes(emptyEthaneAccount)
	updateTrieForEthane(restoreAddr, data)
}

// InspectTrieWithinRange prints trie stats (size, node num, node types, depths) within range
func InspectTrieWithinRange(stateRoot common.Hash, startKey, endKey []byte) string {
	// fmt.Println("stateRoot:", stateRoot.Hex(), startKey, endKey)

	leafNodeDepths := []uint{}
	trieNodeStat := inspectTrieWithinRange(stateRoot, 0, &leafNodeDepths, "", startKey, endKey)

	// deal with depths
	var avgDepth float64
	minDepth := uint(100)
	maxDepth := uint(0)
	depthSum := uint(0)
	for _, depth := range leafNodeDepths {
		depthSum += depth
		if maxDepth < depth {
			maxDepth = depth
		}
		if minDepth > depth {
			minDepth = depth
		}
	}
	if len(leafNodeDepths) != 0 {
		avgDepth = float64(depthSum) / float64(len(leafNodeDepths))
		fmt.Println("avg depth:", avgDepth, "( min:", minDepth, "/ max:", maxDepth, ")")
	} else {
		minDepth = 0
		avgDepth = 0
		maxDepth = 0
		fmt.Println("avg depth: there is no leaf node")
	}

	// print result
	trieNodeStat.Print()
	result := trieNodeStat.ToString(" ") + strconv.FormatUint(uint64(minDepth), 10) + " " + fmt.Sprintf("%f", avgDepth) + " " + strconv.FormatUint(uint64(maxDepth), 10)
	fmt.Println(result)
	fmt.Println()
	return result
}

func inspectEthaneTries(blockNum uint64) {
	fmt.Println("inspectEthaneTries() at block", blockNum)
	stateRoot := common.Blocks[blockNum].Root
	inactiveBoundaryKey := common.InactiveBoundaryKeys[blockNum]

	//
	// print trie stats (size, node num, node types, depths)
	//
	startKey := trie.KeybytesToHex(common.HexToHash("0x0").Bytes())
	inactiveBoundary := trie.KeybytesToHex(common.Uint64ToHash(inactiveBoundaryKey).Bytes())
	inactiveBoundarySubOne := trie.KeybytesToHex(common.Uint64ToHash(inactiveBoundaryKey - 1).Bytes())
	endKey := trie.KeybytesToHex(common.HexToHash("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff").Bytes())

	// total trie
	fmt.Println()
	fmt.Println("print total trie")
	totalResult := InspectTrieWithinRange(stateRoot, startKey, endKey)

	// active trie
	fmt.Println()
	fmt.Println("print active trie")
	activeResult := InspectTrieWithinRange(stateRoot, inactiveBoundary, endKey)

	// inactive trie
	fmt.Println()
	fmt.Println("print inactive trie")
	inactiveResult := InspectTrieWithinRange(stateRoot, startKey, inactiveBoundarySubOne)

	fmt.Println(totalResult, activeResult, inactiveResult)
}

// print Ethane related stats
func printEthaneState() {

	fmt.Println("current block num:", common.CurrentBlockNum)

	// print options
	fmt.Println("delete epoch:", common.DeleteEpoch, "/ inactivate epoch:", common.InactivateEpoch, "/ inactivate criterion:", common.InactivateCriterion)

	fmt.Println("remained keys to delete:", len(common.KeysToDelete))

	// active addresses
	fmt.Println("active accounts:", len(common.AddrToKeyActive))

	// inactive addresses
	fmt.Println("inactive addresses:", len(common.AddrToKeyInactive))
	fmt.Println("inactive boundary key:", common.InactiveBoundaryKey)

	// current NextKey = # of state trie updates
	fmt.Println("current NextKey:", common.NextKey, "/ NextKey(hex):", common.Uint64ToHash(common.NextKey).Hex())

	// print total/active/inactive trie stats (size, node num, node types, depths)
	inspectEthaneTries(common.CurrentBlockNum)
	latestCheckpointBlockNum := common.CurrentBlockNum - (common.CurrentBlockNum % common.InactivateEpoch) - 1
	inspectEthaneTries(latestCheckpointBlockNum)     // relatively small state trie after delete/inactivate
	inspectEthaneTries(latestCheckpointBlockNum - 1) // relatively large state trie before delete/inactivate
}

func setEthaneOptions(deleteEpoch, inactivateEpoch, inactivateCriterion uint64) {
	common.DeleteEpoch = deleteEpoch
	common.InactivateEpoch = inactivateEpoch
	common.InactivateCriterion = inactivateCriterion
}

func getTrieLastKey() {
	fmt.Println("getTrieLastKey:", normTrie.GetLastKey())
	fmt.Printf("getTrieLastKey hex: %x", normTrie.GetLastKey())
}

func connHandler(conn net.Conn) {
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
				fmt.Println("current block num:", common.CurrentBlockNum)
				normTrie.Print()
				response = []byte("success")

			case "inspectDB":
				fmt.Println("execute inspectDB()")
				totalNodesNum, totalNodesSize := common.TotalNodeStat.GetSum()
				totalStorageNodesNum, totalStorageNodesSize := common.TotalStorageNodeStat.GetSum()
				fmt.Println("print inspectDB result: state trie")
				common.TotalNodeStat.Print()
				fmt.Println("print inspectDB result: storage tries")
				common.TotalStorageNodeStat.Print()

				nodeNum := totalNodesNum + totalStorageNodesNum
				nodeSize := totalNodesSize + totalStorageNodesSize
				response = []byte(strconv.FormatUint(nodeNum, 10) + "," + strconv.FormatUint(nodeSize, 10))

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

			case "inspectTrieWithinRange":
				fmt.Println("execute InspectTrieWithinRange()")
				rootHash := normTrie.Hash()
				fmt.Println("root hash:", rootHash.Hex())

				startKey := trie.KeybytesToHex(common.HexToHash("0x0").Bytes())
				endKey := trie.KeybytesToHex(common.HexToHash("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff").Bytes())
				result := InspectTrieWithinRange(rootHash, startKey, endKey)

				fmt.Println("print InspectTrieWithinRange result")
				fmt.Println(result)

				response = []byte("success")

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
				newStorageNodesNum, newStorageNodesSize := common.NewStorageNodeStat.GetSum()
				nodeSum := newNodesNum + newStorageNodesNum
				nodeSize := newNodesSize + newStorageNodesSize
				response = []byte(strconv.FormatUint(nodeSum, 10) + "," + strconv.FormatUint(nodeSize, 10))

			case "updateTrieSimple":
				key, err := hex.DecodeString(params[1]) // convert hex string to bytes
				if err != nil {
					fmt.Println("ERROR: failed decoding")
					response = []byte("ERROR: fail decoding while updateTrie")
				} else {
					// fmt.Println("execute updateTrieSimple() -> key:", common.Bytes2Hex(key))
					// encoding account
					emptyAccount.Nonce++ // to make different leaf node
					data, _ := rlp.EncodeToBytes(emptyAccount)
					err = updateTrie(key, data)
					if err == nil {
						// fmt.Println("success updateTrie -> new root hash:", normTrie.Hash().Hex())
						response = []byte("success updateTrieSimple")
					} else {
						// this is not normal case, need to fix certain error
						fmt.Println("ERROR: fail updateTrieSimple:", err, "/ key:", params[1])
						os.Exit(1)
					}
				}
				normTrie.Hash()
				// normTrie.Print()
				// fmt.Println("\n\n")

			case "updateTrie":
				// get params
				// fmt.Println("execute updateTrie()")
				nonce, _ := strconv.ParseUint(params[1], 10, 64)
				balance := new(big.Int)
				balance, _ = balance.SetString(params[2], 10)
				root := common.HexToHash(params[3])
				codeHash := common.Hex2Bytes(params[4])
				addr := common.HexToAddress(params[5])
				addrHash := crypto.Keccak256Hash(addr[:])
				// fmt.Println("nonce:", nonce)
				// fmt.Println("balance:", balance)
				// fmt.Println("root:", root)
				// fmt.Println("codeHash:", codeHash)
				// fmt.Println("codeHashHex:", common.Bytes2Hex(codeHash))
				// fmt.Println("addr:", addr.Hex())
				// fmt.Println("addrHash:", addrHash.Hex())

				// encoding account
				var account types.StateAccount
				account.Nonce = nonce
				account.Balance = balance
				account.Root = root
				account.CodeHash = codeHash
				data, _ := rlp.EncodeToBytes(account)

				err := updateTrie(addrHash[:], data)
				if err != nil {
					fmt.Println("updateTrie() failed:", err)
					os.Exit(1)
				}
				normTrie.Hash()
				// fmt.Println("print state trie\n")
				// normTrie.Print()

				response = []byte("success")

			case "updateStorageTrie":
				// get params
				// fmt.Println("execute updateStorageTrie()")

				contractAddr := common.HexToAddress(params[1])
				slot := common.HexToHash(params[2])
				value := common.HexToHash(params[3])
				// fmt.Println("contractAddr:", contractAddr)
				// fmt.Println("slot:", slot)
				// fmt.Println("value:", value)
				// fmt.Println()

				// get storage trie to update
				storageTrie, doExist := dirtyStorageTries[contractAddr]
				if !doExist {
					// get storage trie root
					addrHash := crypto.Keccak256Hash(contractAddr[:])
					enc, err := normTrie.TryGet(addrHash[:])
					if err != nil {
						fmt.Println("TryGet() error:", err)
						os.Exit(1)
					}
					var acc types.StateAccount
					if err := rlp.DecodeBytes(enc, &acc); err != nil {
						fmt.Println("Failed to decode state object:", err)
						fmt.Println("contractAddr:", contractAddr)
						fmt.Println("enc:", enc)
						fmt.Println("this address might be newly appeared address")
						acc.Root = common.Hash{}
					}

					// open storage trie
					storageTrie, err = trie.NewSecure(acc.Root, trie.NewDatabase(diskdb))
					if err != nil {
						fmt.Println("trie.NewSecure() failed:", err)
						fmt.Println("contractAddr:", contractAddr)
						fmt.Println("acc.Root:", acc.Root.Hex())
						os.Exit(1)
					}

					// add to dirty storage tries
					dirtyStorageTries[contractAddr] = storageTrie
				}

				// update storage trie
				updateStorageTrie(storageTrie, slot, value)

				response = []byte(storageTrie.Hash().Hex())

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
				common.TrieGraph.Features = make(map[string]map[string]string)

				// inspect trie before graph generation
				root := normTrie.Hash()
				inspectTrie(root)

				// collect graph info
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

			case "findShortestPrefixAmongShortNodes":
				fmt.Println("execute findShortestPrefixAmongShortNodes()")
				maxPrefixesNum, err := strconv.ParseUint(params[1], 10, 64)
				fmt.Println("maxPrefixesNum:", maxPrefixesNum)
				if err != nil {
					fmt.Println("ParseUint error:", err)
					os.Exit(1)
				}

				// get shortest prefixes
				shortestPrefixes, success := findShortestPrefixAmongShortNodes(normTrie.Hash())

				// pick prefixes among them (caution: this may contain duplications)
				prefixes := make([]string, 0)
				if uint64(len(shortestPrefixes)) <= maxPrefixesNum {
					prefixes = shortestPrefixes
				} else {
					mrand.Seed(time.Now().Unix()) // initialize global pseudo random generator
					for i := uint64(0); i < maxPrefixesNum; i++ {
						randIndex := mrand.Intn(len(shortestPrefixes))
						prefixes = append(prefixes, shortestPrefixes[randIndex])
					}
				}

				resp := ""
				cnt := 0
				for _, prefix := range prefixes {
					resp += prefix + ","
					cnt++
					if len(resp) > maxResponseLen-100 {
						// fmt.Println("cutted -> include only", i, "prefixes / # of prefixes:", len(prefixes), "/ len of prefix:", len(prefixes[0]))
						break
					}
				}
				resp += strconv.FormatBool(success)
				response = []byte(resp)
				fmt.Println("include (", cnt, "/", len(shortestPrefixes), ") prefixes / len of prefix:", len(prefixes[0]))

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
				totalStorageResult := common.TotalStorageNodeStat.ToString(delimiter)
				subTrieResult := nodeInfo.SubTrieNodeStat.ToString(delimiter)

				fmt.Print(totalResult)
				fmt.Print(totalStorageResult)
				fmt.Print(subTrieResult)
				fmt.Println("@")

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
				logData := totalResult + totalStorageResult + subTrieResult
				fmt.Fprintln(logFile, logData)
				logFile.Close()

				response = []byte("success")

			case "switchSimulationMode":
				fmt.Println("execute updateTrieForEthane()")
				mode, _ := strconv.ParseUint(params[1], 10, 64)
				if mode == 0 {
					common.IsEthane = false
				} else if mode == 1 {
					common.IsEthane = true
				} else {
					fmt.Println("Error switchSimulationMode: invalid mode ->", mode)
					os.Exit(1)
				}

				response = []byte("success")

			case "updateTrieForEthane":
				// get params
				// fmt.Println("execute updateTrieForEthane()")
				nonce, _ := strconv.ParseUint(params[1], 10, 64)
				balance := new(big.Int)
				balance, _ = balance.SetString(params[2], 10)
				root := common.HexToHash(params[3])
				codeHash := common.Hex2Bytes(params[4])
				addr := common.HexToAddress(params[5])
				// fmt.Println("nonce:", nonce)
				// fmt.Println("balance:", balance)
				// fmt.Println("root:", root)
				// fmt.Println("codeHash:", codeHash)
				// fmt.Println("codeHashHex:", common.Bytes2Hex(codeHash))
				// fmt.Println("addr:", addr.Hex())

				// encoding account
				var ethaneAccount types.EthaneStateAccount
				ethaneAccount.Nonce = nonce
				ethaneAccount.Balance = balance
				ethaneAccount.Root = root
				ethaneAccount.CodeHash = codeHash
				ethaneAccount.Addr = addr
				data, _ := rlp.EncodeToBytes(ethaneAccount)

				updateTrieForEthane(addr, data)
				normTrie.Hash()
				// fmt.Println("print state trie\n")
				// normTrie.Print()

				response = []byte("success")

			case "updateTrieForEthaneSimple":
				// get params
				// fmt.Println("execute updateTrieForEthaneSimple()")
				addr := common.HexToAddress(params[1])
				fmt.Println("addr:", addr.Hex())

				// encoding account: just raise state update with meaningless data
				emptyEthaneAccount.Addr = addr
				data, _ := rlp.EncodeToBytes(emptyEthaneAccount)
				emptyEthaneAccount.Nonce++

				updateTrieForEthane(addr, data)
				normTrie.Hash()
				// fmt.Println("print state trie\n")
				// normTrie.Print()

				response = []byte("success")

			case "printEthaneState":
				fmt.Println("execute printEthaneState()")
				printEthaneState()
				response = []byte("success")

			case "getTrieLastKey":
				normTrie.Print()
				getTrieLastKey()
				response = []byte("success")

			case "setEthaneOptions":
				deleteEpoch, _ := strconv.ParseUint(params[1], 10, 64)
				inactivateEpoch, _ := strconv.ParseUint(params[2], 10, 64)
				inactivateCriterion, _ := strconv.ParseUint(params[3], 10, 64)
				setEthaneOptions(deleteEpoch, inactivateEpoch, inactivateCriterion)

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
		fmt.Println("\nwait for requests...")
		conn, err := listener.Accept()
		if err != nil {
			log.Println(err)
			continue
		}
		defer conn.Close()

		// go ConnHandler(conn) // asynchronous
		connHandler(conn) // synchronous
	}

}
