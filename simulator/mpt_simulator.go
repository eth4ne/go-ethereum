package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
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

	// choose leveldb vs memorydb
	useLeveldb = true
	// leveldb path
	leveldbPath = "/home/jmlee/ssd/mptSimulator/trieNodes"
	// leveldb cache size (MB)
	leveldbCache = 10240
	// leveldb options
	leveldbHandles   = 524288
	leveldbNamespace = "eth/db/chaindata/"
	leveldbReadonly  = false
)

var (
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
	common.NewTrieNodesNum = 0
	common.NewTrieNodesSize = 0
	common.TotalTrieNodesNum = 0
	common.TotalTrieNodesSize = 0
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

			// update db stats
			common.TotalTrieNodesNum--
			common.TotalTrieNodesSize -= uint64(common.TrieNodeInfos[nodeHash].Size)

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
	common.NewTrieNodesNum = uint64(0)
	common.NewTrieNodesSize = uint64(0)

	// print db stats
	fmt.Println("  rollback finished -> total trie nodes:", common.TotalTrieNodesNum, "/ db size:", common.TotalTrieNodesSize)

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
	common.NewTrieNodesNum = 0
	common.NewTrieNodesSize = 0

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
	fmt.Println("  new trie nodes:", common.NewTrieNodesNum, "/ increased db size:", common.NewTrieNodesSize)
	fmt.Println("  total trie nodes:", common.TotalTrieNodesNum, "/ db size:", common.TotalTrieNodesSize)
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
func inspectTrie(hash common.Hash) (uint64, uint64) {

	// find this node's info
	nodeInfo, isFlushedNode := common.TrieNodeInfos[hash]
	if !isFlushedNode {
		var exist bool
		nodeInfo, exist = common.TrieNodeInfosDirty[hash]
		if !exist {
			// this node is unknown, just return 0s
			return 0, 0
		}
	}

	// dynamic programming approach
	if nodeInfo.SubTrieNodesNum != 0 {
		return nodeInfo.SubTrieNodesNum + 1, uint64(nodeInfo.Size) + nodeInfo.SubTrieSize
	}

	// measure trie node num & size
	subTrieNodeNum := uint64(0)
	subTrieSize := uint64(0)
	for _, childHash := range nodeInfo.ChildHashes {
		num, size := inspectTrie(childHash)
		subTrieNodeNum += num
		subTrieSize += size
	}

	// memorize trie node num & size
	nodeInfo.SubTrieNodesNum = subTrieNodeNum
	nodeInfo.SubTrieSize = subTrieSize
	if isFlushedNode {
		common.TrieNodeInfos[hash] = nodeInfo
	} else {
		common.TrieNodeInfosDirty[hash] = nodeInfo
	}

	return subTrieNodeNum + 1, uint64(nodeInfo.Size) + subTrieSize
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
			response := make([]byte, 4096)
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

			case "inspectDB":
				fmt.Println("execute inspectDB()")
				response = []byte(strconv.FormatUint(common.TotalTrieNodesNum, 10) + "," + strconv.FormatUint(common.TotalTrieNodesSize, 10))

			case "inspectTrie":
				fmt.Println("execute inspectTrie()")
				currentTrieNodeNum, currentTrieSize := inspectTrie(normTrie.Hash())
				fmt.Println("trie node num:", currentTrieNodeNum, "/ trie size:", currentTrieSize, "B")
				response = []byte(strconv.FormatUint(currentTrieNodeNum, 10) + "," + strconv.FormatUint(currentTrieSize, 10))

			case "flush":
				fmt.Println("execute flushTrieNodes()")
				flushTrieNodes()
				response = []byte(strconv.FormatUint(common.NewTrieNodesNum, 10) + "," + strconv.FormatUint(common.NewTrieNodesSize, 10))

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
						fmt.Println("ERROR: fail updateTrie")
						response = []byte("ERROR: fail updateTrie")
					}
				}

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
				currentTrieNum, currentTrieSize := inspectTrie(common.Blocks[common.CurrentBlockNum].Root)
				newTrieNum, newTrieSize := inspectTrie(normTrie.Hash())
				incTrieNum := newTrieNum - currentTrieNum
				incTrieSize := newTrieSize - currentTrieSize

				incTotalNodesNum, incTotalNodesSize := estimateIncrement(normTrie.Hash())

				response = []byte(strconv.FormatUint(incTrieNum, 10) +
					"," + strconv.FormatUint(incTrieSize, 10) +
					"," + strconv.FormatUint(incTotalNodesNum, 10) +
					"," + strconv.FormatUint(incTotalNodesSize, 10))

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
		// fmt.Println("  total trie nodes:", common.TotalTrieNodesNum, "/ db size:", common.TotalTrieNodesSize)
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
		go ConnHandler(conn)
	}

}
