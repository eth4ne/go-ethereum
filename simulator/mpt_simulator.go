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
	"github.com/ethereum/go-ethereum/core/state/pruner"
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
	// block infos log file path
	blockInfosLogFilePath = "./blockInfos/"

	// trie graph json file path
	trieGraphPath = "./trieGraphs/"

	// choose leveldb vs memorydb
	useLeveldb = true
	// leveldb path ($ sudo chmod -R 777 /ethereum)
	leveldbPath = "/ethereum/mptSimulator_jmlee/trieNodes/port_" + serverPort
	// leveldb cache size (MB) (Geth default: 512) (memory leak might occur when calling reset() frequently with too big cache size)
	leveldbCache = 20000
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
	// additional inactive trie for Ethane / cached trie for Ethanos
	subNormTrie *trie.Trie
	// secure state trie
	secureTrie *trie.SecureTrie
	// storage tries which have unflushed trie updates
	// dirtyStorageTries[contractAddr] = opened storage trie
	// (reset dirtyStorageTries after flush)
	dirtyStorageTries = make(map[common.Address]*trie.SecureTrie)

	// same as SecureTrie.hashKeyBuf (to mimic SecureTrie)
	hashKeyBuf = make([]byte, common.HashLength)

	// timer to measure block process time
	flushStartTime time.Time
)

// for Merkle proof (from core/state/statedb.go)
type proofList [][]byte

func (n *proofList) Put(key []byte, value []byte) error {
	*n = append(*n, value)
	return nil
}

func (n *proofList) Delete(key []byte) error {
	panic("not supported")
}

func reset(deleteDisk bool) {

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
		if deleteDisk {
			fmt.Println("delete disk, open new disk")
			err := os.RemoveAll(leveldbPath)
			if err != nil {
				fmt.Println("RemoveAll error ! ->", err)
			}
		} else {
			fmt.Println("do not delete disk, open old db if it exist")
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
	subNormTrie, _ = trie.New(common.Hash{}, trie.NewDatabase(diskdb))

	// reset secure trie
	secureTrie, _ = trie.NewSecure(common.Hash{}, trie.NewDatabase(memorydb.New()))

	// reset db stats
	common.TotalNodeStat.Reset()
	common.TotalStorageNodeStat.Reset()
	common.NewNodeStat.Reset()
	common.NewStorageNodeStat.Reset()

	common.TrieNodeHashes = make(map[common.Hash]struct{})
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
	common.NextBlockNum = 0

	// reset Ethane related vars
	common.AddrToKeyActive = make(map[common.Address]common.Hash)
	common.AddrToKeyInactive = make(map[common.Address][]common.Hash)
	common.RestoredAddresses = make(map[common.Address]struct{})
	common.CrumbAddresses = make(map[common.Address]struct{})
	common.NextKey = uint64(0)
	common.CheckpointKey = uint64(0)
	common.KeysToDelete = make([]common.Hash, 0)
	common.InactiveBoundaryKey = uint64(0)
	common.CheckpointKeys = make(map[uint64]uint64)
	common.RestoredKeys = make([]common.Hash, 0)
	common.BlockRestoreStat.Reset()
	common.DeletedActiveNodeNum = uint64(0)
	common.DeletedInactiveNodeNum = uint64(0)

	// TODO(jmlee): init for Ethanos
	// reset Ethanos related vars
	pruner.InitBloomFilters()

	// set block flush timer
	flushStartTime = time.Now()
}

// rollbackUncommittedUpdates goes back to latest flushed trie (i.e., undo all uncommitted trie updates)
// discards unflushed dirty nodes
func rollbackUncommittedUpdates() {
	// there is no block, just reset
	if common.NextBlockNum == 0 {
		reset(true)
		return
	}

	latestBlockNum := common.NextBlockNum - 1

	// open current state trie
	currentRoot := common.Blocks[latestBlockNum].Root
	normTrie, _ = trie.New(currentRoot, trie.NewDatabase(diskdb))

	// rollback account nonce value
	emptyAccount.Nonce = common.Blocks[latestBlockNum].MaxAccountNonce

	// drop all dirty nodes
	common.TrieNodeInfosDirty = make(map[common.Hash]common.NodeInfo)
}

// rollback goes back to the target block state
func rollbackToBlock(targetBlockNum uint64) bool {

	if common.NextBlockNum == 0 {
		fmt.Println("cannot rollback, no block exist")
		return false
	}

	latestBlockNum := common.NextBlockNum - 1

	// check rollback validity
	if targetBlockNum >= latestBlockNum {
		fmt.Println("cannot rollback to current or future")
		return false
	}
	if _, exist := common.Blocks[targetBlockNum]; !exist {
		fmt.Println("cannot rollback to target block: target block info is deleted (rollback limit)")
		return false
	}
	if targetBlockNum == 0 {
		// just execute reset(), when we rollback to block 0
		reset(true)
		return true
	}

	// rollback db modifications
	fmt.Println("rollbackToBlock: from block", latestBlockNum, "to block", targetBlockNum)
	for blockNum := latestBlockNum; blockNum > targetBlockNum; blockNum-- {
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
		common.NextBlockNum--
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

	bn := common.NextBlockNum
	fmt.Println("\nflush start: generate block number", bn, "( port:", serverPort, ")")
	blockInfo, _ := common.Blocks[bn] // for logging block info

	// if Ethane simulation, do inactivate or delete previous leaf nodes
	if common.SimulationMode == 1 {

		// update checkpoint key
		common.CheckpointKeys[bn] = common.CheckpointKey
		common.CheckpointKey = common.NextKey

		// delete
		if (bn+1)%common.DeleteEpoch == 0 {
			deleteStartTime := time.Now()
			deletePrevLeafNodes()
			deleteElapsed := time.Since(deleteStartTime)
			blockInfo.TimeToDelete = deleteElapsed.Nanoseconds()
			// fmt.Println("time to delete:", blockInfo.TimeToDelete, "ns")
		}

		// inactivate
		if (bn+1)%common.InactivateEpoch == 0 && bn != common.InactivateEpoch-1 {
			inactivateStartTime := time.Now()

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

					inactivateElapsed := time.Since(inactivateStartTime)
					blockInfo.TimeToInactivate = inactivateElapsed.Nanoseconds()
					// fmt.Println("time to inactivate:", blockInfo.TimeToInactivate, "ns")
				}

			}
		}

		// store inactive boundary key in this block
		common.InactiveBoundaryKeys[bn] = common.InactiveBoundaryKey
	}

	// logging address stats for Ethane
	blockInfo.ActiveAddressNum = len(common.AddrToKeyActive) - len(common.RestoredAddresses) - len(common.CrumbAddresses)
	blockInfo.RestoredAddressNum = len(common.RestoredAddresses)
	blockInfo.CrumbAddressNum = len(common.CrumbAddresses)
	blockInfo.InactiveAddressNum = len(common.AddrToKeyInactive)

	// reset db stats before flush
	common.NewNodeStat.Reset()
	common.NewStorageNodeStat.Reset()

	// flush state trie nodes
	common.FlushStorageTries = false
	normTrie.Commit(nil)
	normTrie.TrieDB().Commit(normTrie.Hash(), false, nil)
	if common.SimulationMode == 1 && common.InactiveTrieExist {
		subNormTrie.Commit(nil)
		subNormTrie.TrieDB().Commit(subNormTrie.Hash(), false, nil)
		blockInfo.InactiveRoot = subNormTrie.Hash()
	}
	if common.SimulationMode == 2 {
		// this is cached trie's root for Ethanos
		blockInfo.InactiveRoot = subNormTrie.Hash()
	}

	// flush storage trie nodes
	common.FlushStorageTries = true
	for _, storageTrie := range dirtyStorageTries {
		storageTrie.Commit(nil)
		storageTrie.TrieDB().Commit(storageTrie.Hash(), false, nil)
	}
	// reset dirty storage tries after flush
	dirtyStorageTries = make(map[common.Address]*trie.SecureTrie)

	// logging flush time
	blockInfo.TimeToFlush = time.Since(flushStartTime).Nanoseconds()
	flushStartTime = time.Now()
	// fmt.Println("time to flush:", blockInfo.TimeToFlush, "ns")

	// update block info (to be able to rollback to this state later)
	// fmt.Println("new trie root after flush:", normTrie.Hash().Hex())
	blockInfo.Root = normTrie.Hash()
	blockInfo.MaxAccountNonce = emptyAccount.Nonce
	blockInfo.NewNodeStat = common.NewNodeStat
	blockInfo.NewStorageNodeStat = common.NewStorageNodeStat
	blockInfo.TotalNodeStat = common.TotalNodeStat
	blockInfo.TotalStorageNodeStat = common.TotalStorageNodeStat
	blockInfo.BlockRestoreStat = common.BlockRestoreStat
	common.BlockRestoreStat.Reset()
	blockInfo.DeletedActiveNodeNum = common.DeletedActiveNodeNum
	blockInfo.DeletedInactiveNodeNum = common.DeletedInactiveNodeNum
	blockInfo.InactivatedNodeNum = common.InactiveBoundaryKey
	common.Blocks[bn] = blockInfo
	// delete too old block to store
	blockNumToDelete := bn - common.MaxBlocksToStore - 1
	if _, exist := common.Blocks[blockNumToDelete]; exist {
		// fmt.Println("current block num:", bn, "/ block num to delete:", blockNumToDelete)
		delete(common.Blocks, blockNumToDelete)
	}

	// discard unflushed dirty nodes after flush
	common.TrieNodeInfosDirty = make(map[common.Hash]common.NodeInfo)

	// show db stat
	// totalNodesNum, totalNodesSize := common.TotalNodeStat.GetSum()
	// totalStorageNodesNum, totalStorageNodesSize := common.TotalStorageNodeStat.GetSum()
	// newNodesNum, newNodesSize := common.NewNodeStat.GetSum()
	// newStorageNodesNum, newStorageNodesSize := common.NewStorageNodeStat.GetSum()
	// common.NewNodeStat.Print()
	// fmt.Println("  new trie nodes:", newNodesNum, "/ increased db size:", newNodesSize)
	// fmt.Println("  new storage trie nodes:", newStorageNodesNum, "/ increased db size:", newStorageNodesSize)
	// fmt.Println("  total trie nodes:", totalNodesNum, "/ db size:", totalNodesSize)
	// fmt.Println("  total storage trie nodes:", totalStorageNodesNum, "/ db size:", totalStorageNodesSize)

	// if Ethanos simulation, sweep state trie
	if common.SimulationMode == 2 {
		if (bn+1)%common.InactivateCriterion == 0 {
			sweepStateTrie()
		}
	}

	// increase next block number after flush
	common.NextBlockNum++
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

// inspectDatabase iterates db and returns # of total trie nodes and their size
// (use this function when common.CollectNodeInfos, common.CollectNodeHashes are both false
// else, just get db stats from common.TotalNodeStat, common.TotalStorageNodeStat)
func inspectDatabase(diskdb ethdb.KeyValueStore) (uint64, uint64) {
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
		if len(key) == 32 {
			// this is trie node
			totalNodes++
			totalSize += size
		}
	}
	fmt.Println("total nodes:", totalNodes, "/ total size:", totalSize, "(", uint64(totalSize), "B )")
	return totalNodes, uint64(totalSize)
}

// inspectTrieDisk measures number and size of trie nodes in the trie (get node info from disk)
func inspectTrieDisk(hash common.Hash) common.NodeStat {

	// open trie to inspect
	trieToInsepct, err := trie.New(hash, trie.NewDatabase(diskdb))
	if err != nil {
		fmt.Println("inspectTrieDisk() err:", err)
		os.Exit(1)
	}

	// inspect trie
	tir := trieToInsepct.InspectTrie()
	// tir.PrintTrieInspectResult(0, 0)

	return tir.ToNodeStat()
}

// inspectTrieMem measures number and size of trie nodes in the trie (get node info from memory)
// (count duplicated nodes)
func inspectTrieMem(hash common.Hash) {
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
		inspectTrieMem(childHash)

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

// inspectTrieWithinRangeMem measures number and size of trie nodes in the sub trie
// (count duplicated nodes)
// do not utilize dynamic programming, just keep searching until it reaches to leaf nodes
func inspectTrieWithinRangeMem(hash common.Hash, depth uint, leafNodeDepths *[]uint, prefix string, startKey, endKey []byte) common.NodeStat {
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
			childNodeStat = inspectTrieWithinRangeMem(childHash, depth+1, leafNodeDepths, prefix+nodeInfo.Indices[i], startKey, endKey)
		} else {
			// this node is short node
			childNodeStat = inspectTrieWithinRangeMem(childHash, depth+1, leafNodeDepths, prefix, startKey, endKey)
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
// TODO(jmlee): memorize shortest prefixes (maybe add NodeInfo.shortestPrefixes field)
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
// functions for Ethanos
//

// TODO(jmlee): implement this
func updateTrieForEthanos(addr common.Address, key, value []byte) error {
	// update state trie
	err := normTrie.TryUpdate(key, value)
	if err != nil {
		return err
	}

	// update bloom filter with address
	pruner.LatestBloomFilter.Add(addr.Bytes())

	return nil
}

// TODO(jmlee): implement this correctly
// current version just finds latest account state and set restored flag (similar to Ethane's)
// to be correct, need to search checkpoint block's tries and bloom filters
// (to measure restore proof's size)
// ex. how to use bloom filter
// exist, _ := pruner.LatestBloomFilter.Contain(restoreAddr.Bytes())
func restoreAccountForEthanos(restoreAddr common.Address) {

	blockNum := common.NextBlockNum
	currentEpochNum := int(blockNum / common.InactivateCriterion)
	if currentEpochNum < 2 {
		fmt.Println("ERROR: restoration is not needed at epoch 0~1")
		fmt.Println("restore address:", restoreAddr.Hex())
		// os.Exit(1) // exit to check restore list's correctness
		return
	}

	// check whether this address exists at each checkpoint block (epoch: 0 ~ cached)
	exists := make([]bool, currentEpochNum)

	// iterate checkpoint blocks (to know whether this address exists or not)
	addrHash := crypto.Keccak256Hash(restoreAddr[:])
	epochNum := currentEpochNum - 1 // we do not have to prove current state trie's account
	// fmt.Println("current epoch:", currentEpochNum, "/ epochNum:", epochNum)
	for ; epochNum >= 0; epochNum-- {

		// open checkpoint block's state trie
		checkpointBlockNum := uint64((epochNum+1))*common.InactivateCriterion - 1
		checkpointRoot := common.Blocks[checkpointBlockNum].Root
		checkpointTrie, err := trie.New(checkpointRoot, trie.NewDatabase(diskdb))
		if err != nil {
			fmt.Println("restoreAccountForEthanos() error:", err)
			os.Exit(1)
		}

		// search checkpoint block's state trie
		enc, _ := checkpointTrie.TryGet(addrHash[:])
		if enc != nil {
			exists[epochNum] = true

			// decode account
			var acc types.EthanosStateAccount
			if err := rlp.DecodeBytes(enc, &acc); err != nil {
				fmt.Println("Failed to decode state object:", err)
				fmt.Println("restoreAddr:", restoreAddr)
				fmt.Println("enc:", enc)
				os.Exit(1)
			}

			// we do not need states older than last restoration, stop searching
			if acc.Restored {
				break
			}
		}
	}
	if epochNum == -1 {
		epochNum = 0
	}
	// fmt.Println("after iterate, epochNum:", epochNum)

	//
	// find first epoch to make proofs
	//
	// ignore sequential void proofs
	for ; epochNum < len(exists); epochNum++ {
		if exists[epochNum] {
			break
		}
	}
	// ignore sequential membership proofs without last one just before void proof appears
	for ; epochNum < len(exists)-1; epochNum++ {
		if !exists[epochNum+1] {
			break
		}
	}
	if epochNum >= currentEpochNum-1 {
		// if start epoch num is newer than cached checkpoint, then this address do not need restoration
		fmt.Println("ERROR: we only needs cached trie to restore, which means this address do not need restoration")
		fmt.Println("restore address:", restoreAddr.Hex())
		fmt.Println("currentEpochNum:", currentEpochNum, "/ epochNum:", epochNum)

		if common.StopWhenErrorOccurs {
			os.Exit(1)
		}
		return
	}

	// collect proofs for restoration
	trie.RestoreProofHashes = make(map[common.Hash]int) // reset multi proof
	for ; epochNum < currentEpochNum-1; epochNum++ {
		// check bloom filter first
		exist, _ := pruner.BloomFilters[epochNum].Contain(restoreAddr.Bytes())
		if !exist {
			// bloom filter can be void proof
			// fmt.Println("at epoch", epochNum, ": bloom")
			common.BlockRestoreStat.BloomFilterNum++
			continue
		} else {
			// need merkle proof
			// fmt.Println("at epoch", epochNum, ": merkle")
			common.BlockRestoreStat.MerkleProofNum++
		}

		// open checkpoint block's state trie
		checkpointBlockNum := uint64((epochNum+1))*common.InactivateCriterion - 1
		checkpointRoot := common.Blocks[checkpointBlockNum].Root
		checkpointTrie, err := trie.New(checkpointRoot, trie.NewDatabase(diskdb))
		if err != nil {
			fmt.Println("restoreAccountForEthanos() error:", err)
			os.Exit(1)
		}

		// get Merkle proof for this account
		var proof proofList
		checkpointTrie.Prove(addrHash[:], common.FromLevel, &proof)
		if exists[epochNum] {
			// this is membership proof
			common.BlockRestoreStat.RestoredAccountNum++
		} else {
			// this is void proof
		}

	}

	// var for experiment: no need to merge accounts, actually
	// because latest account is the latest state of this address in our simulation
	var latestAcc types.EthanosStateAccount
	var enc []byte
	// search current state trie first
	enc, _ = normTrie.TryGet(addrHash[:])
	if enc == nil {
		// search checkpoint tries
		for i := len(exists) - 1; i >= 0; i-- {
			if exists[i] {
				checkpointBlockNum := uint64((i+1))*common.InactivateCriterion - 1
				checkpointRoot := common.Blocks[checkpointBlockNum].Root
				checkpointTrie, err := trie.New(checkpointRoot, trie.NewDatabase(diskdb))
				if err != nil {
					fmt.Println("restoreAccountForEthanos() error:", err)
					os.Exit(1)
				}

				enc, _ = checkpointTrie.TryGet(addrHash[:])
				// fmt.Println("find latest account at epoch", i)
				break
			}
		}
	}
	// decode account
	if err := rlp.DecodeBytes(enc, &latestAcc); err != nil {
		fmt.Println("Failed to decode state object:", err)
		fmt.Println("restoreAddr:", restoreAddr)
		fmt.Println("exists:", exists)
		fmt.Println("enc:", enc)
		os.Exit(1)
	}
	// set restored flag
	latestAcc.Restored = true
	// encode restored account
	data, _ := rlp.EncodeToBytes(latestAcc)
	// insert to current state trie
	updateTrieForEthanos(restoreAddr, addrHash[:], data)

	// logging restore stats
	// RESTORE_ALL: restore all inactive leaf nodes of this address
	nodeNum, proofSize := trie.GetMultiProofStats()
	common.BlockRestoreStat.RestorationNum++
	common.BlockRestoreStat.MerkleProofsSize += proofSize
	common.BlockRestoreStat.MerkleProofsNodesNum += nodeNum
	if common.BlockRestoreStat.MaxProofSize < proofSize {
		common.BlockRestoreStat.MaxProofSize = proofSize
	}
	if common.BlockRestoreStat.MinProofSize > proofSize || common.BlockRestoreStat.MinProofSize == 0 {
		common.BlockRestoreStat.MinProofSize = proofSize
	}

	// fmt.Println("restoreAccountForEthanos for", restoreAddr.Hex(), "finish")
	// fmt.Println("exists:", exists)
	// fmt.Println("multi proof stats -> nodeNum:", nodeNum, "/ proofSize:", proofSize, "(B)")
}

// TODO(jmlee): implement this
func sweepStateTrie() {
	// set cached trie
	subNormTrie = normTrie

	// open new empty state trie
	normTrie, _ = trie.New(common.Hash{}, trie.NewDatabase(diskdb))

	// save current bloom filter, and make new one for new epoch
	pruner.SweepBloomFilter()
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
		if _, isCrumb := common.AddrToKeyInactive[addr]; isCrumb {
			// this is crumb address
			common.CrumbAddresses[addr] = struct{}{}
			// since we always restore all inactive accounts, crumb address cannot be restored address
			delete(common.RestoredAddresses, addr) // (but this may not be happen in RESTORE_ALL scenario)
		} else {
			// this is new address
		}

		// set addrKey for this address
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

	// logging
	common.DeletedActiveNodeNum += uint64(len(common.KeysToDelete))

	// init keys to delete
	common.KeysToDelete = make([]common.Hash, 0)
}

// delete already used merkle proofs in inactive trie
func deleteRestoredLeafNodes() {
	// check if inactive trie exist
	inactiveTrie := normTrie
	if common.InactiveTrieExist {
		inactiveTrie = subNormTrie
		if common.DoLightInactiveDeletion {
			common.DeletingInactiveTrieFlag = true // for Ethane's light inactive trie delete (jmlee)
		}
	}

	// delete all used merkle proofs for restoration
	for _, keyToDelete := range common.RestoredKeys {
		err := inactiveTrie.TryDelete(keyToDelete[:])
		if err != nil {
			fmt.Println("ERROR deleteRestoredLeafNodes(): trie.TryDelete ->", err)
			fmt.Println("  but this might be due to light inactive trie deletion")

			// try once again after hashing nodes
			// this might be due to light inactive trie deletion
			inactiveTrie.Hash()
			err := inactiveTrie.TryDelete(keyToDelete[:])
			if err != nil {
				fmt.Println("ERROR deleteRestoredLeafNodes() again: trie.TryDelete ->", err)
				os.Exit(1)
			}
		}
	}
	common.DeletingInactiveTrieFlag = false

	// logging
	common.DeletedInactiveNodeNum += uint64(len(common.RestoredKeys))

	// init keys to delete
	common.RestoredKeys = make([]common.Hash, 0)
}

// move inactive accounts from active trie to inactive trie
func inactivateLeafNodes(firstKeyToCheck, lastKeyToCheck common.Hash) {
	fmt.Println("inactivate leaf nodes() executed")

	// check if inactive trie exist
	inactiveTrie := normTrie
	if common.InactiveTrieExist {
		inactiveTrie = subNormTrie
	}

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
		err = inactiveTrie.TryUpdate(keyToInsert[:], accountsToInactivate[index])
		if err != nil {
			fmt.Println("inactivateLeafNodes insert error:", err)
			os.Exit(1)
		}

		// update AddrToKey (active & inactive)
		delete(common.AddrToKeyActive, addr)
		common.AddrToKeyInactive[addr] = append(common.AddrToKeyInactive[addr], keyToInsert)

		// delete from restored & crumb addresses if exist
		delete(common.RestoredAddresses, addr)
		delete(common.CrumbAddresses, addr)
	}
}

// restore all inactive accounts of given address (for Ethane)
// TODO(jmlee): check correctness
// TODO(jmlee): measure multi proof size
func restoreAccountForEthane(restoreAddr common.Address) {
	// check if inactive trie exist
	inactiveTrie := normTrie
	if common.InactiveTrieExist {
		inactiveTrie = subNormTrie
	}

	if len(common.AddrToKeyInactive[restoreAddr]) == 0 {
		fmt.Println("there is no account to restore")
		if common.StopWhenErrorOccurs {
			os.Exit(1)
		}
		return
	}

	//
	// collect & merge inactive accounts
	//

	var restoredAcc types.EthaneStateAccount
	restoredAcc.Balance = big.NewInt(0)
	restoredAcc.Root = emptyRoot
	restoredAcc.CodeHash = emptyCodeHash

	// reset multi proof
	trie.RestoreProofHashes = make(map[common.Hash]int)
	merkleProofNum := 0

	// var for experiment: no need to merge accounts, actually
	// because latest account is the latest state of this address in our simulation
	var latestAcc types.EthaneStateAccount

	for _, key := range common.AddrToKeyInactive[restoreAddr] {

		// get inactive account
		enc, err := inactiveTrie.TryGet(key[:])
		if err != nil {
			fmt.Println("TryGet() error:", err)
			os.Exit(1)
		}
		var acc types.EthaneStateAccount
		if err := rlp.DecodeBytes(enc, &acc); err != nil {
			fmt.Println("Failed to decode state object:", err)
			fmt.Println("restoreAddr:", restoreAddr)
			fmt.Println("enc:", enc)
			os.Exit(1)
		}
		latestAcc = acc

		// get Merkle proof for this account
		var proof proofList
		inactiveTrie.Prove(key[:], common.FromLevel, &proof)
		merkleProofNum++

		// merge account
		restoredAcc.Balance.Add(restoredAcc.Balance, acc.Balance)
		if restoredAcc.Nonce < acc.Nonce {
			restoredAcc.Nonce = acc.Nonce
		}
		if acc.Root != emptyRoot {
			restoredAcc.Root = acc.Root
		}
		if bytes.Compare(acc.CodeHash, emptyCodeHash) != 0 {
			restoredAcc.CodeHash = acc.CodeHash
		}
	}

	// get active account if exist & merge it
	addrKey, exist := common.AddrToKeyActive[restoreAddr]
	if exist {
		// get active account
		enc, err := normTrie.TryGet(addrKey[:])
		if err != nil {
			fmt.Println("TryGet() error:", err)
			os.Exit(1)
		}
		var acc types.EthaneStateAccount
		if err := rlp.DecodeBytes(enc, &acc); err != nil {
			fmt.Println("Failed to decode state object:", err)
			fmt.Println("restoreAddr:", restoreAddr)
			fmt.Println("enc:", enc)
			os.Exit(1)
		}
		latestAcc = acc

		// merge account
		restoredAcc.Balance.Add(restoredAcc.Balance, acc.Balance)
		if restoredAcc.Nonce < acc.Nonce {
			restoredAcc.Nonce = acc.Nonce
		}
		if acc.Root != emptyRoot {
			restoredAcc.Root = acc.Root
		}
		if bytes.Compare(acc.CodeHash, emptyCodeHash) != 0 {
			restoredAcc.CodeHash = acc.CodeHash
		}
	}

	// insert restored account
	// data, _ := rlp.EncodeToBytes(restoredAcc)
	data, _ := rlp.EncodeToBytes(latestAcc) // code for experiment
	updateTrieForEthane(restoreAddr, data)

	// logging restore stats
	// RESTORE_ALL: restore all inactive leaf nodes of this address
	nodeNum, proofSize := trie.GetMultiProofStats()
	common.BlockRestoreStat.RestorationNum++
	common.BlockRestoreStat.RestoredAccountNum += len(common.AddrToKeyInactive[restoreAddr])
	common.BlockRestoreStat.MerkleProofNum += len(common.AddrToKeyInactive[restoreAddr])
	common.BlockRestoreStat.MerkleProofsSize += proofSize
	common.BlockRestoreStat.MerkleProofsNodesNum += nodeNum
	if common.BlockRestoreStat.MaxProofSize < proofSize {
		common.BlockRestoreStat.MaxProofSize = proofSize
	}
	if common.BlockRestoreStat.MinProofSize > proofSize || common.BlockRestoreStat.MinProofSize == 0 {
		common.BlockRestoreStat.MinProofSize = proofSize
	}

	// record merkle proofs for restoration (to delete these later)
	common.RestoredKeys = append(common.RestoredKeys, common.AddrToKeyInactive[restoreAddr]...)
	delete(common.AddrToKeyInactive, restoreAddr)

	// add to RestoredAddresses
	common.RestoredAddresses[restoreAddr] = struct{}{}
	// since we always restore all inactive accounts, restored address cannot be crumb address
	delete(common.CrumbAddresses, restoreAddr)

	// fmt.Println("success restore:", restoreAddr.Hex())

	// fmt.Println("inactive root:", inactiveTrie.Hash().Hex())
	// fmt.Println("multi proof stats -> merkleNum: ", merkleProofNum, "/ nodeNum:", nodeNum, "/ proofSize:", proofSize, "(B)")
}

// InspectTrieWithinRange prints trie stats (size, node num, node types, depths) within range
func InspectTrieWithinRange(stateRoot common.Hash, startKey, endKey []byte) string {
	// fmt.Println("stateRoot:", stateRoot.Hex(), startKey, endKey)

	if common.CollectNodeInfos {
		// inspect trie in memory (get node info from memory)

		leafNodeDepths := []uint{}
		trieNodeStat := inspectTrieWithinRangeMem(stateRoot, 0, &leafNodeDepths, "", startKey, endKey)
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

	} else {
		// TODO(jmlee): implement inspecting in disk

		// inspect trie in disk (get node info from disk)
		return ""
	}

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
	if inactiveBoundaryKey == 0 {
		// there is no inactive account
		inactiveBoundarySubOne = startKey
	}
	endKey := trie.KeybytesToHex(common.HexToHash("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff").Bytes())

	var totalResult, activeResult, inactiveResult string

	if common.InactiveTrieExist {
		// active trie
		fmt.Println()
		fmt.Println("print active trie at block num", blockNum)
		activeNodeStat := inspectTrieDisk(stateRoot)
		// activeNodeStat.Print()
		_ = activeNodeStat

		// inactive trie
		fmt.Println()
		fmt.Println("print inactive trie at block num", blockNum)
		inactiveRoot := common.Blocks[blockNum].InactiveRoot
		inactiveNodeStat := inspectTrieDisk(inactiveRoot)
		// inactiveNodeStat.Print()
		_ = inactiveNodeStat
	} else {
		// TODO(jmlee): implement below
		_, _, _ = inactiveBoundary, inactiveBoundarySubOne, endKey

		// // total trie
		// fmt.Println()
		// fmt.Println("print total trie at block num", blockNum)
		// totalNodeStat := inspectTrieDisk(stateRoot)
		// totalResult = totalNodeStat.ToString(" ")

		// // active trie part
		// fmt.Println()
		// fmt.Println("print active trie part at block num", blockNum)
		// activeResult := InspectTrieWithinRange(stateRoot, inactiveBoundary, endKey)

		// // inactive trie part
		// fmt.Println()
		// fmt.Println("print inactive trie at block num", blockNum)
		// if common.InactiveTrieExist {
		// 	inactiveResult = InspectTrieWithinRange(subNormTrie.Hash(), startKey, inactiveBoundarySubOne)
		// } else {
		// 	inactiveResult = InspectTrieWithinRange(stateRoot, startKey, inactiveBoundarySubOne)
		// }
		// if inactiveBoundaryKey == 0 {
		// 	// there is no inactive account
		// 	inactiveResult = "0 0 0 0 0 0 0 0 0 0 0"
		// }
	}

	fmt.Println(totalResult, activeResult, inactiveResult)
}

// print Ethane related stats
func printEthaneState() {

	if common.NextBlockNum == 0 {
		fmt.Println("there is no block to print state, just finish printEthaneState()")
		return
	}
	latestBlockNum := common.NextBlockNum - 1
	fmt.Println("latest block num:", latestBlockNum)

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

	fmt.Println("deleted nodes in inactive trie:", common.DeletedInactiveNodeNum)
	fmt.Println("deleted nodes in active trie:", common.DeletedActiveNodeNum)

	// print total/active/inactive trie stats (size, node num, node types, depths)
	// inspectEthaneTries(latestBlockNum)
	// latestCheckpointBlockNum := latestBlockNum - (latestBlockNum % common.InactivateEpoch) - 1
	// inspectEthaneTries(latestCheckpointBlockNum)     // relatively small state trie after delete/inactivate
	// inspectEthaneTries(latestCheckpointBlockNum - 1) // relatively large state trie before delete/inactivate
}

func setEthaneOptions(deleteEpoch, inactivateEpoch, inactivateCriterion uint64, fromLevel uint) {
	common.DeleteEpoch = deleteEpoch
	common.InactivateEpoch = inactivateEpoch
	common.InactivateCriterion = inactivateCriterion
	common.FromLevel = fromLevel
	fmt.Println("setEthaneOptions() finish -> mode:", common.SimulationMode)
	fmt.Println("  DeleteEpoch:", common.DeleteEpoch)
	fmt.Println("  InactivateEpoch:", common.InactivateEpoch)
	fmt.Println("  InactivateCriterion:", common.InactivateCriterion)
	fmt.Println("  FromLevel:", common.FromLevel)
}

// save block infos
func saveBlockInfos(fileName string, startBlockNum, endBlockNum uint64) {
	// delete prev log file if exist
	os.Remove(blockInfosLogFilePath + fileName)
	// open new log file
	f, _ := os.OpenFile(blockInfosLogFilePath+fileName, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	defer f.Close()

	delimiter := " "
	for blockNum := startBlockNum; blockNum <= endBlockNum; blockNum++ {
		blockInfo, exist := common.Blocks[blockNum]
		if !exist {
			fmt.Println("there is no block", blockNum)
			os.Exit(1)
		}

		// generate log
		log := ""
		log += blockInfo.Root.Hex() + delimiter
		log += blockInfo.InactiveRoot.Hex() + delimiter
		log += strconv.FormatUint(blockNum, 10) + delimiter

		log += blockInfo.NewNodeStat.ToString(delimiter)
		log += blockInfo.NewStorageNodeStat.ToString(delimiter)
		log += blockInfo.TotalNodeStat.ToString(delimiter)
		log += blockInfo.TotalStorageNodeStat.ToString(delimiter)

		log += strconv.FormatInt(blockInfo.TimeToFlush, 10) + delimiter
		log += strconv.FormatInt(blockInfo.TimeToDelete, 10) + delimiter
		log += strconv.FormatInt(blockInfo.TimeToInactivate, 10) + delimiter

		log += blockInfo.BlockRestoreStat.ToString(delimiter)

		log += strconv.FormatUint(blockInfo.DeletedActiveNodeNum, 10) + delimiter
		log += strconv.FormatUint(blockInfo.DeletedInactiveNodeNum, 10) + delimiter
		log += strconv.FormatUint(blockInfo.InactivatedNodeNum, 10) + delimiter

		log += strconv.Itoa(blockInfo.ActiveAddressNum) + delimiter
		log += strconv.Itoa(blockInfo.RestoredAddressNum) + delimiter
		log += strconv.Itoa(blockInfo.CrumbAddressNum) + delimiter
		log += strconv.Itoa(blockInfo.InactiveAddressNum) + delimiter

		// TODO(jmlee): merge these to BlockRestoreStat.ToString() later
		log += strconv.Itoa(blockInfo.BlockRestoreStat.MinProofSize) + delimiter
		log += strconv.Itoa(blockInfo.BlockRestoreStat.MaxProofSize) + delimiter

		// fmt.Println("log at block", blockNum, ":", log)

		// write log to file
		fmt.Fprintln(f, log)
	}

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
				deleteDisk, _ := strconv.ParseUint(params[1], 10, 64)
				if deleteDisk == 0 {
					reset(false)
				} else {
					reset(true)
				}

				response = []byte("reset success")

			case "getBlockNum":
				// caution: this returns next block num, not latest block num
				fmt.Println("execute getBlockNum()")
				response = []byte(strconv.FormatUint(common.NextBlockNum, 10))

			case "getTrieRootHash":
				// fmt.Println("execute getTrieRootHash()")
				rootHash := normTrie.Hash().Hex()
				// fmt.Println("current trie root hash:", rootHash)
				response = []byte(rootHash)

			case "printCurrentTrie":
				fmt.Println("execute printCurrentTrie()")
				normTrie.Hash()
				fmt.Println("next block num:", common.NextBlockNum)
				normTrie.Print()
				response = []byte("success")

			case "getDBStatistics":
				// caution: this statistics might be wrong, use inspectDatabase to get correct values
				fmt.Println("\nexecute getDBStatistics()")
				if !common.CollectNodeInfos && !common.CollectNodeHashes {
					fmt.Println("  CAUTION: this statistics might contain several same trie nodes")
				}
				totalNodesNum, totalNodesSize := common.TotalNodeStat.GetSum()
				totalStorageNodesNum, totalStorageNodesSize := common.TotalStorageNodeStat.GetSum()
				fmt.Println("print getDBStatistics result: all state trie nodes in disk (archive data)")
				common.TotalNodeStat.Print()
				fmt.Println(common.TotalNodeStat.ToString(" "))
				fmt.Println("print getDBStatistics result: all storage tries nodes in disk (archive data)")
				common.TotalStorageNodeStat.Print()
				fmt.Println(common.TotalStorageNodeStat.ToString(" "))
				fmt.Println()

				nodeNum := totalNodesNum + totalStorageNodesNum
				nodeSize := totalNodesSize + totalStorageNodesSize
				response = []byte(strconv.FormatUint(nodeNum, 10) + "," + strconv.FormatUint(nodeSize, 10))

			case "inspectDatabase":
				fmt.Println("execute inspectDatabase()")

				// inspectDatabase(diskdb) is same as
				// rawdb.InspectDatabase(rawdb.NewDatabase(diskdb), nil, nil)
				inspectDatabase(diskdb)

				response = []byte("success")

			case "inspectTrie":
				fmt.Println("execute inspectTrie()")
				rootHash := normTrie.Hash()
				fmt.Println("root hash:", rootHash.Hex())

				var nodeStat common.NodeStat
				if common.CollectNodeInfos {
					inspectTrieMem(rootHash)
					var nodeInfo common.NodeInfo
					var exist bool
					nodeInfo, exist = common.TrieNodeInfos[rootHash]
					if !exist {
						nodeInfo, exist = common.TrieNodeInfosDirty[rootHash]
					}
					nodeStat = nodeInfo.SubTrieNodeStat
					fmt.Println("print inspectTrie result")
					nodeStat.Print()
				} else {
					nodeStat = inspectTrieDisk(rootHash)
				}

				response = []byte(rootHash.Hex() + "," + nodeStat.ToStringWithDepths(","))

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
				fmt.Println("execute inspectSubTrie()")
				rootHash := common.HexToHash(params[1])
				fmt.Println("rootHash:", rootHash.Hex())

				var nodeStat common.NodeStat
				if common.CollectNodeInfos {
					inspectTrieMem(rootHash)
					var nodeInfo common.NodeInfo
					var exist bool
					nodeInfo, exist = common.TrieNodeInfos[rootHash]
					if !exist {
						nodeInfo, exist = common.TrieNodeInfosDirty[rootHash]
					}
					nodeStat = nodeInfo.SubTrieNodeStat
				} else {
					nodeStat = inspectTrieDisk(rootHash)
				}

				fmt.Println("print inspectSubTrie result")
				nodeStat.Print()

				delimiter := " "
				response = []byte(nodeStat.ToStringWithDepths(delimiter))

			case "flush":
				// fmt.Println("\nexecute flushTrieNodes()")
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

				// fix storageRoot when our simulation data is wrong (for unstoppable experiment)
				if common.AcceptWrongStorageTrie && root != emptyRoot {

					// check if there is dirty storage trie
					storageTrie, doExist := dirtyStorageTries[addr]
					if doExist {
						if root != storageTrie.Hash() {
							// fmt.Println("simulation made wrong storage trie, fix this 1")
							// fmt.Println("  at block:", common.NextBlockNum)
							// fmt.Println("  addr:", addr)
							// fmt.Println("  correct root:", root)
							// fmt.Println("  actual but wrong root:", storageTrie.Hash())

							root = storageTrie.Hash()
						}
					} else {
						// check if this account has wrong storage trie root
						// if it has, then just maintain that root
						// if tx data is correct, we don't need to fix storage roots like this
						enc, err := normTrie.TryGet(addrHash[:])
						if err != nil {
							fmt.Println("TryGet() error:", err)
							os.Exit(1)
						}
						if enc != nil {
							var acc types.StateAccount
							if err := rlp.DecodeBytes(enc, &acc); err != nil {
								fmt.Println("Failed to decode state object in updateTrie():", err)
								fmt.Println("addr:", addr)
								fmt.Println("enc:", enc)
								os.Exit(1)
							}
							if root != acc.Root {
								// fmt.Println("simulation made wrong storage trie, fix this 2")
								// fmt.Println("  at block:", common.NextBlockNum)
								// fmt.Println("  addr:", addr)
								// fmt.Println("  correct root:", root)
								// fmt.Println("  actual but wrong root:", acc.Root)

								root = acc.Root
							}
						}
					}
				}

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

				// fmt.Println("print state trie\n")
				// normTrie.Hash()
				// normTrie.Print()

				response = []byte("success")

			case "updateTrieDelete":
				// get params
				// fmt.Println("execute updateTrieDelete()")
				addr := common.HexToAddress(params[1])
				addrHash := crypto.Keccak256Hash(addr[:])
				normTrie.TryDelete(addrHash[:])
				// fmt.Println("root after delete:", normTrie.Hash().Hex())

				response = []byte("success")

			case "updateTrieDeleteForEthane":
				// get params
				// fmt.Println("execute updateTrieDeleteForEthane()")
				addr := common.HexToAddress(params[1])

				// get addrKey of this address
				addrKey, exist := common.AddrToKeyActive[addr]
				if exist {
					// delete this account from active state trie
					normTrie.TryDelete(addrKey[:])
					delete(common.AddrToKeyActive, addr)

					// delete from restored & crumb addresses if exist
					delete(common.RestoredAddresses, addr)
					delete(common.CrumbAddresses, addr)
				}

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
					storageRoot := common.Hash{}
					addrHash := crypto.Keccak256Hash(contractAddr[:])
					enc, err := normTrie.TryGet(addrHash[:])
					if err != nil {
						fmt.Println("TryGet() error:", err)
						os.Exit(1)
					}
					if enc != nil {
						var acc types.StateAccount
						if err := rlp.DecodeBytes(enc, &acc); err != nil {
							fmt.Println("Failed to decode state object in updateStorageTrie():", err)
							fmt.Println("contractAddr:", contractAddr)
							fmt.Println("enc:", enc)
							os.Exit(1)
						}
						storageRoot = acc.Root
					}

					// open storage trie
					storageTrie, err = trie.NewSecure(storageRoot, trie.NewDatabase(diskdb))
					if err != nil {
						fmt.Println("trie.NewSecure() failed:", err)
						fmt.Println("contractAddr:", contractAddr)
						fmt.Println("storageRoot:", storageRoot.Hex())
						os.Exit(1)
					}

					// add to dirty storage tries
					dirtyStorageTries[contractAddr] = storageTrie
				}

				// update storage trie
				updateStorageTrie(storageTrie, slot, value)

				response = []byte(storageTrie.Hash().Hex())

			case "updateStorageTrieEthane":
				// get params
				// fmt.Println("execute updateStorageTrieEthane()")

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

					// find addrKey
					addrKey, exist := common.AddrToKeyActive[contractAddr]
					var acc types.EthaneStateAccount
					acc.Root = common.Hash{}
					if exist {
						// find account
						enc, err := normTrie.TryGet(addrKey[:])
						if err != nil {
							fmt.Println("TryGet() error:", err)
							os.Exit(1)
						}
						if err := rlp.DecodeBytes(enc, &acc); err != nil {
							fmt.Println("Failed to decode state object in updateStorageTrieEthane():", err)
							fmt.Println("contractAddr:", contractAddr)
							fmt.Println("enc:", enc)
							fmt.Println("ERROR: AddrToKeyActive exists but account is not there")
							os.Exit(1)
						}
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

			case "updateStorageTrieEthanos":
				// get params
				// fmt.Println("execute updateStorageTrieEthanos()")

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
					storageRoot := common.Hash{}
					var acc types.EthanosStateAccount
					addrHash := crypto.Keccak256Hash(contractAddr[:])
					enc, err := normTrie.TryGet(addrHash[:])
					if err != nil {
						fmt.Println("TryGet() error:", err)
						os.Exit(1)
					}
					if enc != nil {
						if err := rlp.DecodeBytes(enc, &acc); err != nil {
							fmt.Println("Failed to decode state object in updateStorageTrie():", err)
							fmt.Println("contractAddr:", contractAddr)
							fmt.Println("enc:", enc)
							os.Exit(1)
						}
						storageRoot = acc.Root
					} else {
						// check cached trie
						enc, err := subNormTrie.TryGet(addrHash[:])
						if err != nil {
							fmt.Println("TryGet() error:", err)
							os.Exit(1)
						}
						if enc != nil {
							if err := rlp.DecodeBytes(enc, &acc); err != nil {
								fmt.Println("Failed to decode state object in updateStorageTrie():", err)
								fmt.Println("contractAddr:", contractAddr)
								fmt.Println("enc:", enc)
								os.Exit(1)
							}
							storageRoot = acc.Root
						}
					}

					// open storage trie
					storageTrie, err = trie.NewSecure(storageRoot, trie.NewDatabase(diskdb))
					if err != nil {
						fmt.Println("trie.NewSecure() failed:", err)
						fmt.Println("contractAddr:", contractAddr)
						fmt.Println("storageRoot:", storageRoot.Hex())
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
				var flushedNodesNum, flushedNodesSize uint64
				if common.NextBlockNum == 0 {
					flushedNodesNum, flushedNodesSize = 0, 0
				} else {
					latestBlockNum := common.NextBlockNum - 1
					flushedRoot := common.Blocks[latestBlockNum].Root
					inspectTrieMem(flushedRoot)
					flushedRootInfo, existF := common.TrieNodeInfos[flushedRoot]
					if !existF {
						flushedRootInfo, existF = common.TrieNodeInfosDirty[flushedRoot]
					}
					flushedNodesNum, flushedNodesSize = flushedRootInfo.SubTrieNodeStat.GetSum()
				}

				// inspect current trie
				currentRoot := normTrie.Hash()
				inspectTrieMem(currentRoot)
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
				inspectTrieMem(root)

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
				var nodeStat common.NodeStat
				if common.CollectNodeInfos {
					inspectTrieMem(rootHash)
					var nodeInfo common.NodeInfo
					var exist bool
					nodeInfo, exist = common.TrieNodeInfos[rootHash]
					if !exist {
						nodeInfo, exist = common.TrieNodeInfosDirty[rootHash]
					}
					nodeStat = nodeInfo.SubTrieNodeStat
				} else {
					nodeStat = inspectTrieDisk(rootHash)
				}

				totalResult := common.TotalNodeStat.ToString(delimiter)
				totalStorageResult := common.TotalStorageNodeStat.ToString(delimiter)
				subTrieResult := nodeStat.ToString(delimiter)

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

			// TODO(jmlee): call this periodically for safe experiment
			case "saveBlockInfos":
				fmt.Println("execute saveBlockInfos()")
				logFileName := params[1]
				fmt.Println("block infos log file name:", logFileName)
				saveBlockInfos(logFileName, 0, common.NextBlockNum-1)

				response = []byte("success")

			case "switchSimulationMode":
				fmt.Println("execute switchSimulationMode()")
				mode, _ := strconv.ParseUint(params[1], 10, 64)

				if mode == 0 || mode == 1 || mode == 2 {
					common.SimulationMode = int(mode)
				} else {
					fmt.Println("Error switchSimulationMode: invalid mode ->", mode)
					os.Exit(1)
				}

				response = []byte("success")

			case "acceptWrongStorageTrie":
				fmt.Println("execute acceptWrongStorageTrie()")
				option, _ := strconv.ParseUint(params[1], 10, 64)

				if option == 0 {
					common.AcceptWrongStorageTrie = false
				} else if option == 1 {
					common.AcceptWrongStorageTrie = true
				} else {
					fmt.Println("Error acceptWrongStorageTrie: invalid option ->", option)
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

			case "restoreAccountForEthane":
				// get params
				// fmt.Println("execute restoreAccountForEthane()")
				addr := common.HexToAddress(params[1])
				fmt.Println("addr to restore:", addr.Hex())

				restoreAccountForEthane(addr)

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
				fromLevel, _ := strconv.ParseUint(params[4], 10, 64)
				setEthaneOptions(deleteEpoch, inactivateEpoch, inactivateCriterion, uint(fromLevel))

				response = []byte("success")

			case "updateTrieForEthanos":
				// get params
				// fmt.Println("execute updateTrieForEthanos()")
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

				// read Restored field from cached trie
				restored := false
				enc, _ := subNormTrie.TryGet(addrHash[:])
				if enc != nil {
					// decode latest account
					var acc types.EthanosStateAccount
					if err := rlp.DecodeBytes(enc, &acc); err != nil {
						fmt.Println("Failed to decode state object:", err)
						fmt.Println("addr:", addr)
						fmt.Println("enc:", enc)
						os.Exit(1)
					}
					restored = acc.Restored
				}

				// encoding account
				var account types.EthanosStateAccount
				account.Nonce = nonce
				account.Balance = balance
				account.Root = root
				account.CodeHash = codeHash
				account.Restored = restored
				data, _ := rlp.EncodeToBytes(account)

				err := updateTrieForEthanos(addr, addrHash[:], data)
				if err != nil {
					fmt.Println("updateTrieForEthanos() failed:", err)
					os.Exit(1)
				}
				normTrie.Hash()
				// fmt.Println("print state trie\n")
				// normTrie.Print()

				response = []byte("success")

			case "restoreAccountForEthanos":
				// get params
				// fmt.Println("execute restoreAccountForEthanos()")
				addr := common.HexToAddress(params[1])
				// fmt.Println("addr to restore:", addr.Hex())

				restoreAccountForEthanos(addr)

				response = []byte("success")

			case "setHead":
				// get params
				// fmt.Println("execute setHead()")
				blockNum, _ := strconv.ParseUint(params[1], 10, 64)
				if blockNum >= common.NextBlockNum {
					fmt.Println("cannot setHead with future block")
					response = []byte("fail")
					break
				}

				oldTrieRoot := common.Blocks[blockNum].Root
				normTrie, err = trie.New(oldTrieRoot, trie.NewDatabase(diskdb))
				if err != nil {
					fmt.Println("setHead() request err:", err)
					fmt.Println("requested blockNum:", blockNum)
					fmt.Println("requested rootHash:", oldTrieRoot.Hex())
					os.Exit(1)
				}
				fmt.Println("open new norm trie:", normTrie.Hash().Hex())
				common.NextBlockNum = blockNum + 1

				response = []byte("success")

			case "convertEthaneToEthereum":
				// convert Ethane's state as Ethereum's (to check simulation correctness)

				// get params
				fmt.Println("execute convertEthaneToEthereum()")

				// 1. restore all inactive accounts
				for inactiveAddr, _ := range common.AddrToKeyInactive {
					restoreAccountForEthane(inactiveAddr)
				}

				// 2. delete prev leaf nodes
				deletePrevLeafNodes()

				// 3. find all accounts in active trie
				startKey := common.HexToHash("0x0").Bytes()
				endKey := common.HexToHash("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff").Bytes()
				allAccounts, _, _ := normTrie.FindLeafNodes(startKey[:], endKey[:])
				newEthereumTrie, _ := trie.New(common.Hash{}, trie.NewDatabase(diskdb))

				// 4. insert all accounts to secure trie like Ethereum
				for _, EthaneAccountBytes := range allAccounts {
					// get Ethane account
					var ethaneAcc types.EthaneStateAccount
					if err := rlp.DecodeBytes(EthaneAccountBytes, &ethaneAcc); err != nil {
						fmt.Println("Failed to decode state object:", err)
						fmt.Println("enc:", EthaneAccountBytes)
						os.Exit(1)
					}

					// convert to Ethereum account
					var ethereumAcc types.StateAccount
					ethereumAcc.Balance = ethaneAcc.Balance
					ethereumAcc.Nonce = ethaneAcc.Nonce
					ethereumAcc.CodeHash = ethaneAcc.CodeHash
					ethereumAcc.Root = ethaneAcc.Root
					addrHash := crypto.Keccak256Hash(ethaneAcc.Addr[:])
					data, _ := rlp.EncodeToBytes(ethereumAcc)

					// update state trie
					err := newEthereumTrie.TryUpdate(addrHash[:], data)
					if err != nil {
						fmt.Println("updateTrie fail:", err)
						os.Exit(1)
					}
				}

				fmt.Println("convert success, ethereum trie root:", newEthereumTrie.Hash().Hex())
				response = []byte(newEthereumTrie.Hash().Hex())

			case "convertEthanosToEthereum":
				// convert Ethanos's state as Ethereum's (to check simulation correctness)

				// get params
				fmt.Println("execute convertEthanosToEthereum()")
				// fmt.Println("normTrieRoot:", normTrie.Hash())
				// fmt.Println("subNormTrieRoot:", subNormTrie.Hash())

				// 1. restore all addresses (at python script)

				// 2. insert all cached accounts to secure trie
				startKey := common.HexToHash("0x0").Bytes()
				endKey := common.HexToHash("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff").Bytes()
				normTrie.Hash()
				allAccounts, allAddrHashes, _ := subNormTrie.FindLeafNodes(startKey[:], endKey[:])
				newEthereumTrie, _ := trie.New(common.Hash{}, trie.NewDatabase(diskdb))
				for i := 0; i < len(allAccounts); i++ {
					// get Ethanos account
					var ethanosAcc types.EthanosStateAccount
					if err := rlp.DecodeBytes(allAccounts[i], &ethanosAcc); err != nil {
						fmt.Println("Failed to decode state object:", err)
						fmt.Println("enc:", allAccounts[i])
						os.Exit(1)
					}

					// convert to Ethereum account
					var ethereumAcc types.StateAccount
					ethereumAcc.Balance = ethanosAcc.Balance
					ethereumAcc.Nonce = ethanosAcc.Nonce
					ethereumAcc.CodeHash = ethanosAcc.CodeHash
					ethereumAcc.Root = ethanosAcc.Root
					addrHash := allAddrHashes[i]
					data, _ := rlp.EncodeToBytes(ethereumAcc)

					// update state trie
					err := newEthereumTrie.TryUpdate(addrHash[:], data)
					if err != nil {
						fmt.Println("updateTrie fail:", err)
						os.Exit(1)
					}
				}

				// 3. insert all current accounts to secure trie
				allAccounts, allAddrHashes, _ = normTrie.FindLeafNodes(startKey[:], endKey[:])
				for i := 0; i < len(allAccounts); i++ {
					// get Ethanos account
					var ethanosAcc types.EthanosStateAccount
					if err := rlp.DecodeBytes(allAccounts[i], &ethanosAcc); err != nil {
						fmt.Println("Failed to decode state object:", err)
						fmt.Println("enc:", allAccounts[i])
						os.Exit(1)
					}

					// convert to Ethereum account
					var ethereumAcc types.StateAccount
					ethereumAcc.Balance = ethanosAcc.Balance
					ethereumAcc.Nonce = ethanosAcc.Nonce
					ethereumAcc.CodeHash = ethanosAcc.CodeHash
					ethereumAcc.Root = ethanosAcc.Root
					addrHash := allAddrHashes[i]
					data, _ := rlp.EncodeToBytes(ethereumAcc)

					// update state trie
					err := newEthereumTrie.TryUpdate(addrHash[:], data)
					if err != nil {
						fmt.Println("updateTrie fail:", err)
						os.Exit(1)
					}
				}

				// change normTrie (to delete destructed accounts from trie later, at python script)
				normTrie = newEthereumTrie

				fmt.Println("convert success, ethereum trie root:", newEthereumTrie.Hash().Hex())
				response = []byte(newEthereumTrie.Hash().Hex())

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

// check whether inactive trie can be updated only with its rightmost merkle path nodes
// for Ethane's light inactive trie delete (jmlee)
// just run testLightInactiveTrieUpdate() in main() to test the functionality
func testLightInactiveTrieUpdate() {
	fmt.Println("start testLightInactiveTrieUpdate()")
	for i := 0; i < 16; i++ {
		trie.ZeroHashNode[i] = 15
	}

	// set DBs
	memdb := memorydb.New()
	tempMemdb := memorydb.New()
	archiveMemdb := memorydb.New()

	// store keys to delete restored merkle proofs
	keysToDelete := make(map[common.Hash]struct{})
	allDeletedKeys := make(map[common.Hash]struct{})

	// open tries
	myTrie, _ := trie.New(common.Hash{}, trie.NewDatabase(memdb))             // inactive trie only with its rightmost merkle path nodes
	archiveTrie, _ := trie.New(common.Hash{}, trie.NewDatabase(archiveMemdb)) // total inactive trie

	// set account to be inserted in tries
	var acc types.StateAccount
	acc.Balance = big.NewInt(0)
	acc.Nonce = 0
	acc.CodeHash = emptyCodeHash
	acc.Root = emptyRoot

	cnt := uint64(0)
	totalInsertedAccNum := 0
	totalDeletedAccNum := 0
	var err error
	roundNumToRun := 10000
	currentAccNum := 0
	maxInsertNum := 100
	maxDeleteNum := 85
	for i := 0; i < roundNumToRun; i++ {

		// insert accounts in both tries identically
		mrand.Seed(time.Now().UnixNano())
		randInsertNum := mrand.Intn(maxInsertNum) + 1
		// randInsertNum := 4
		totalInsertedAccNum += randInsertNum
		currentAccNum += randInsertNum
		fmt.Println("\n\n\n\n\n\ninsert :", randInsertNum, "accounts in the trie / i:", i)
		// fmt.Println("archive trie (before)")
		// archiveTrie.Print()
		var addrKey common.Hash
		for j := 0; j < randInsertNum; j++ {
			// update trie
			addrKey = common.HexToHash(strconv.FormatUint(cnt, 16))
			cnt++
			data, _ := rlp.EncodeToBytes(acc)
			acc.Nonce++
			err1 := myTrie.TryUpdate(addrKey[:], data)
			err2 := archiveTrie.TryUpdate(addrKey[:], data)
			if err1 != nil {
				fmt.Println("ERROR: cannot update trie only with rightmost merkle proofs")
				fmt.Println("  => err1:", err1)
				fmt.Println("  => err2:", err2)

				fmt.Println("archive trie (after)")
				archiveTrie.Print()

				os.Exit(1)
			}
		}
		// delete accounts from both tries identically
		fmt.Println("delete :", len(keysToDelete), "accounts")
		common.DeletingInactiveTrieFlag = true
		for key, _ := range keysToDelete {
			// fmt.Println("  key to delete:", key.Hex())
			// fmt.Println("light trie delete start")
			err1 := myTrie.TryDelete(key[:])
			myTrie.Hash() // this might be needed
			// fmt.Println("full trie delete start")
			err2 := archiveTrie.TryDelete(key[:])
			archiveTrie.Hash() // this might be needed
			if err1 != nil {
				fmt.Println("ERROR: cannot delete trie only with rightmost merkle proofs")
				fmt.Println("  => error key to delete:", key.Hex())
				fmt.Println("  => err1:", err1)
				fmt.Println("  => err2:", err2)

				fmt.Println("archive trie (after)")
				archiveTrie.Print()

				os.Exit(1)
			}

			myTrieRoot := myTrie.Hash().Hex()
			archiveTrieRoot := archiveTrie.Hash().Hex()
			if myTrieRoot != archiveTrieRoot {
				fmt.Println("ERROR: myTrie and archiveTrie have different roots (should be same)")
				fmt.Println("myTrieRoot:", myTrieRoot, "/ archiveTrieRoot:", archiveTrieRoot)
				myTrie.Print()
				fmt.Println("%%%%%%%%%%")
				archiveTrie.Print()
				fmt.Println("  key to delete:", key.Hex())
				os.Exit(1)
			}
			currentAccNum--
		}
		common.DeletingInactiveTrieFlag = false
		keysToDelete = make(map[common.Hash]struct{}) // clear map
		// check result
		myTrieRoot := myTrie.Hash().Hex()
		archiveTrieRoot := archiveTrie.Hash().Hex()
		if myTrieRoot != archiveTrieRoot {
			fmt.Println("ERROR: myTrie and archiveTrie have different roots (should be same)")
			fmt.Println("myTrieRoot:", myTrieRoot, "/ archiveTrieRoot:", archiveTrieRoot)
			os.Exit(1)
		}
		// fmt.Println("archive trie (after)")
		// archiveTrie.Print()

		// flush tries
		myTrie.Commit(nil)
		myTrie.TrieDB().Commit(myTrie.Hash(), false, nil)
		archiveTrie.Commit(nil)
		archiveTrie.TrieDB().Commit(archiveTrie.Hash(), false, nil)

		// get rightmost merkle path nodes
		trie.RestoreProofHashes = make(map[common.Hash]int) // reset multi proof
		var proof proofList
		myTrie.Prove(addrKey[:], 0, &proof)
		// for k, _ := range trie.RestoreProofHashes {
		// 	fmt.Println("merkle key:", k.Hex())
		// }
		// get merkle paths to delete
		mrand.Seed(time.Now().UnixNano())
		randDeleteNum := mrand.Intn(maxDeleteNum) + 1
		if currentAccNum < maxDeleteNum {
			randDeleteNum = mrand.Intn(currentAccNum) + 1
		}
		totalDeletedAccNum += randDeleteNum
		proofNumToDelete := 0
		for j := 0; proofNumToDelete < randDeleteNum; j++ {
			mrand.Seed(time.Now().UnixNano())
			randNum := int64(mrand.Intn(int(cnt)))
			randAddrKey := common.HexToHash(strconv.FormatInt(randNum, 16))
			// fmt.Println("(candid) selected key to delete:", randAddrKey.Hex())

			value, _ := archiveTrie.TryGet(randAddrKey[:])
			_, exist1 := keysToDelete[randAddrKey]
			_, exist2 := allDeletedKeys[randAddrKey]
			if value != nil && !exist1 && !exist2 {
				var proof proofList
				archiveTrie.Prove(randAddrKey[:], 0, &proof)
				keysToDelete[randAddrKey] = struct{}{}
				allDeletedKeys[randAddrKey] = struct{}{}
				// fmt.Println("selected key to delete:", randAddrKey.Hex())
				proofNumToDelete++
			}
		}
		merklePathNodeNum := len(trie.RestoreProofHashes)
		fmt.Println("selected nodes num:", merklePathNodeNum)

		// iterate current mem db and only save rightmost merkle path nodes in the new mem db
		it := archiveMemdb.NewIterator(nil, nil)
		insertedNodeNum := 0
		for it.Next() {
			var (
				key   = it.Key()
				value = it.Value()
			)

			if len(key) == 32 {
				keyHash := common.BytesToHash(key)
				if _, exist := trie.RestoreProofHashes[keyHash]; exist {
					// fmt.Println("  key:", keyHash.Hex(), "find! insert to new mem DB")
					tempMemdb.Put(key, value)
					insertedNodeNum++
				}
			}
		}
		if merklePathNodeNum != insertedNodeNum {
			fmt.Println("ERROR: tempMemdb do not has all rightmost merkle path nodes")
			fmt.Println("merklePathNodeNum:", merklePathNodeNum, "/ insertedNodeNum:", insertedNodeNum)
		}

		// now memdb has only rightmost merkle path nodes, and clear new mem db
		memdb = tempMemdb
		tempMemdb = memorydb.New()

		memdbNodeNum, _ := inspectDatabase(memdb)
		tempMemdbNodeNum, _ := inspectDatabase(tempMemdb)
		// archiveMemdbNodeNum, _ := inspectDatabase(archiveMemdb)
		// fmt.Println("memdbNodeNum:", memdbNodeNum, "/ tempMemdbNodeNum:", tempMemdbNodeNum, "/ archiveMemdbNodeNum:", archiveMemdbNodeNum)
		if uint64(merklePathNodeNum) != memdbNodeNum {
			fmt.Println("ERROR: memdb has more/less nodes than nodes in the rightmost merkle path")
			fmt.Println("merklePathNodeNum:", merklePathNodeNum, "/ memdbNodeNum:", memdbNodeNum)
		}
		if tempMemdbNodeNum != 0 {
			fmt.Println("ERROR: tempMemdb is not cleared correctly")
			fmt.Println("tempMemdbNodeNum:", tempMemdbNodeNum)
		}

		// reopen trie which has rightmost merkle path nodes only
		myTrie, err = trie.New(myTrie.Hash(), trie.NewDatabase(memdb))
		if err != nil {
			fmt.Println("ERROR: cannot open trie")
			os.Exit(1)
		}
	}
	// myTrie.Print()

	fmt.Println("SUCCESS: finish test simulation, inactive trie can be updated only with the rightmost merkle proof")
	fmt.Println("  => total inserted accounts:", totalInsertedAccNum)
	fmt.Println("  => total deleted accounts:", totalDeletedAccNum)
	os.Exit(1)
}

// inspect trie in specific db path
// ex. dbPath = "/ethereum/geth/geth/chaindata"
// ex. rootHash = "0x74477eaabece6bce00c346dc12275b2ed74ec9d6c758c4023c2040ba0e72e05d"
func inspectTrieInSpecificPath(dbPath, rootHash string) {

	startTime := time.Now()
	limit, _ := fdlimit.Maximum()
	raised, _ := fdlimit.Raise(uint64(limit))
	leveldbHandles = int(raised / 2)
	fmt.Println("open file limit:", limit, "/ raised:", raised, "/ leveldbHandles:", leveldbHandles)

	diskdb, _ = leveldb.New(dbPath, leveldbCache, leveldbHandles, leveldbNamespace, leveldbReadonly)

	hash := common.HexToHash(rootHash)
	inspectTrieDisk(hash)
	elapsed := time.Since(startTime)
	fmt.Println("inspect trie time:", elapsed)
}

func main() {

	// initialize
	reset(false)

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
