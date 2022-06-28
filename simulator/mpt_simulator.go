package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"io"
	"log"
	"net"
	"strings"
	"math/big"
	// "os"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
)

const (
	// port for requests
	serverPort = "8999"
)

var (
	// empty account
	emptyAccount types.StateAccount
	// emptyCodeHash for EoA accounts
	emptyCodeHash = crypto.Keccak256(nil)
	// emptyRoot is the known root hash of an empty trie.
	emptyRoot = common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")

	// memory db to store trie nodes (should be changed to leveldb instance)
	memDB = memorydb.New()
	// normal state trie
	normTrie *trie.Trie
	// secure state trie
	secureTrie *trie.SecureTrie

	// same as SecureTrie.hashKeyBuf (to mimic SecureTrie)
	hashKeyBuf = make([]byte, common.HashLength)

	// current block number (which will be increased after flushing trie nodes)
	blockNumber = uint64(0)
)

func reset() {
	// reset normal trie
	memDB = memorydb.New()
	normTrie, _ = trie.New(common.Hash{}, trie.NewDatabase(memDB))

	// reset secure trie
	secureTrie, _ = trie.NewSecure(common.Hash{}, trie.NewDatabase(memorydb.New()))

	// reset block number
	blockNumber = 0

	// reset db stats
	common.TrieNodes = make(map[common.Hash]int)
	common.NewTrieNodesNum = 0
	common.NewTrieNodesSize = 0
	common.TotalTrieNodesNum = 0
	common.TotalTrieNodesSize = 0
	common.TrieNodesChildren = make(map[common.Hash][]common.Hash)

	// reset account
	emptyAccount.Balance = big.NewInt(0)
	emptyAccount.Nonce = 0
	emptyAccount.CodeHash = emptyCodeHash
	emptyAccount.Root = emptyRoot
}

// flushTrieNodes flushes dirty trie nodes to the db
func flushTrieNodes() {
	// reset db stats before flush
	common.NewTrieNodes = make(map[common.Hash]struct{})
	common.NewTrieNodesNum = 0
	common.NewTrieNodesSize = 0

	// flush trie nodes
	normTrie.Commit(nil)
	normTrie.TrieDB().Commit(normTrie.Hash(), false, nil)

	// show db stat
	fmt.Println("  new trie nodes:", common.NewTrieNodesNum, "/ increased db size:", common.NewTrieNodesSize)
	fmt.Println("  total trie nodes:", common.TotalTrieNodesNum, "/ db size:", common.TotalTrieNodesSize)
	
	// increase block number
	blockNumber++
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
func inspectDB(memDB *memorydb.Database) (uint64, uint64) {
	// iterate db
	it := memDB.NewIterator(nil, nil)
	totalNodes := uint64(0)
	totalSize := common.StorageSize(0)
	for it.Next() {
		var (
			key  = it.Key()
			size = common.StorageSize(len(key) + len(it.Value()))
		)
		// fmt.Println("node hash:", hex.EncodeToString(key), "/ value:", it.Value(), "/ size: ", size)
		fmt.Println("node hash:", hex.EncodeToString(key), "/ size: ", size)
		totalNodes++
		totalSize += size
	}
	fmt.Println("total nodes:", totalNodes, "/ total size:", totalSize)
	return totalNodes, uint64(totalSize)
}

// inspectCurrentTrie measures number and size of trie nodes in the trie
// (count duplicated nodes)
func inspectCurrentTrie(hash common.Hash) (uint64, uint64) {
	nodeSize := uint64(common.TrieNodes[hash])
	subTrieNodeNum := uint64(0)
	subTrieSize := uint64(0)
	for _, childHash := range common.TrieNodesChildren[hash] {
		num, size := inspectCurrentTrie(childHash)
		subTrieNodeNum += num
		subTrieSize += size
	}
	return subTrieNodeNum + 1, nodeSize + subTrieSize
}

func updateTrie(key []byte) error {
	// encoding value
	emptyAccount.Nonce++ // to make different leaf node
	data, _ := rlp.EncodeToBytes(emptyAccount)

	// update state trie
	// fmt.Println("update trie -> key:", common.BytesToHash(key).Hex())
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
				log.Println(err);
				return
		  	}
		  	log.Println(err);
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
				response = []byte(strconv.FormatUint(blockNumber, 10))

			case "inspectDB":
				fmt.Println("execute inspectDB()")
				response = []byte(strconv.FormatUint(common.TotalTrieNodesNum, 10) + "," + strconv.FormatUint(common.TotalTrieNodesSize, 10))
			
			case "inspectCurrentTrie":
				fmt.Println("execute inspectCurrentTrie()")
				currentTrieNodeNum, currentTrieSize := inspectCurrentTrie(normTrie.Hash())
				fmt.Println("current trie node num:", currentTrieNodeNum, "/ current trie size:", currentTrieSize, "B")
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
	
	normTrie, _ = trie.New(common.Hash{}, trie.NewDatabase(memDB))
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

		fmt.Println("normTrie root:", normTrie.Hash())
		if (i+1) % trieCommitEpoch == 0 {
			flushTrieNodes()
		}
		// fmt.Println("state", i+1)
		// normTrie.Print()
		// fmt.Println("  total trie nodes:", common.TotalTrieNodesNum, "/ db size:", common.TotalTrieNodesSize)
		// fmt.Println("\n\n")
	}

}

func main() {

	// for test, run this function
	// makeTestTrie()

	// initialize
	reset()

	// open tcp socket
	fmt.Println("open socket")
	listener, err := net.Listen("tcp", ":" + serverPort)
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
		   log.Println(err);
		   continue
		}
		defer conn.Close()
		go ConnHandler(conn)
	}

}
