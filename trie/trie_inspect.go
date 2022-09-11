package trie

import (
	"fmt"
	"math/big"
	"os"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

var path = "/home/jhkim/go/src/github.com/ethereum/go-ethereum/txDetail/" // used absolute path
var _ = os.MkdirAll(path, 0777)

func increaseSize(nodeSize int, node string, tir *TrieInspectResult, depth int) {
	rwMutex.Lock()
	defer rwMutex.Unlock()
	tir.TrieSize += nodeSize
	if node == "short" {
		tir.ShortNodeNum++
		tir.ShortNodeSize += nodeSize
		tir.StateTrieShortNodeDepth[depth]++

	} else if node == "full" {
		tir.FullNodeNum++
		tir.FullNodeSize += nodeSize
		tir.StateTrieFullNodeDepth[depth]++

	} else if node == "value" {
		tir.LeafNodeNum++
		tir.LeafNodeSize += nodeSize
		tir.StateTrieLeafNodeDepth[depth]++
	} else {
		fmt.Println("wrong node format in increaseSize")
		os.Exit(1)
	}

}

// check whether slice contains key (jhkim)
func containHash(key common.Hash, slice []common.Hash) bool {
	for _, a := range slice {
		if a == key {
			return true
		}
	}
	return false
}

func writeLogFile(path, data string) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Printf("Cannot create result file.\n")
		os.Exit(1)
	}
	defer f.Close()
	fmt.Fprintln(f, data) // write text file
}

func PrintFlushedNode(blocknumber, distance int) {
	common.GlobalMutex.Lock()
	defer common.GlobalMutex.Unlock()

	ss := fmt.Sprintf("Write FlushedNode to Addr Map %d-%d in txt file", blocknumber-distance, blocknumber)

	filepath := path + "FlushedNode_" + strconv.FormatInt(int64(blocknumber-distance+1), 10) + "-" + strconv.FormatInt(int64(blocknumber), 10) + ".txt"
	f, err := os.Create(filepath)
	if err != nil {
		fmt.Printf("Cannot create result file.\n")
		os.Exit(1)
	}
	defer f.Close()

	start := time.Now()
	tmp := ""
	for key, val := range common.Flushednode_block {
		// fmt.Println("flushednode_block", key)
		s := ""
		s += fmt.Sprintf("  /Block %d:", key)
		cf, cn := 0, 0 // count
		tmp = ""
		for k, v := range val {
			tmp += fmt.Sprintf("\t%v", k)
			if v == 1 {
				tmp += fmt.Sprintf("\t Full Node\n")
				cf += 1
			} else {
				tmp += fmt.Sprintf("\t Short Node\n")
				cn += 1
			}

		}
		s += fmt.Sprintf("\t[Full: %d, Short: %d]\n", cf, cn)
		s += fmt.Sprintln(tmp)

		fmt.Fprintln(f, s)
	}

	// writeLogFile(filepath, s)

	elapsed := time.Since(start)
	ss += fmt.Sprintln(" in", elapsed.Seconds(), "seconds")
	fmt.Print(ss)

	common.Flushednode_block = map[int]map[common.Hash]int{}
}

func PrintAddrhash2Addr(blocknumber, distance int) {
	common.GlobalMutex.Lock()
	defer common.GlobalMutex.Unlock()

	ss := fmt.Sprintf("Write AddressHash to Addr Map %d-%d in txt file", blocknumber-distance, blocknumber)

	filepath := path + "AddrHash2Addr_" + strconv.FormatInt(int64(blocknumber-distance+1), 10) + "-" + strconv.FormatInt(int64(blocknumber), 10) + ".txt"

	f, err := os.Create(filepath)
	if err != nil {
		fmt.Printf("Cannot create result file.\n")
		os.Exit(1)
	}
	defer f.Close()

	start := time.Now()
	for k, v := range common.AddrHash2Addr {

		s := fmt.Sprintf("%v,%v", k, v)
		fmt.Fprintln(f, s)
	}

	elapsed := time.Since(start)
	ss += fmt.Sprintln(" in", elapsed.Seconds(), "seconds")
	fmt.Print(ss)

	common.AddrHash2Addr = map[common.Hash]common.Address{}

	///////////////////////////////////////////////////////////////////////
	// slothash to slot
	ss = fmt.Sprintf("Write SlotHash to Slot Map %d-%d in txt file", blocknumber-distance, blocknumber)

	filepath = path + "Slothash2Slot_" + strconv.FormatInt(int64(blocknumber-distance+1), 10) + "-" + strconv.FormatInt(int64(blocknumber), 10) + ".txt"

	f, err = os.Create(filepath)
	if err != nil {
		fmt.Printf("Cannot create result file.\n")
		os.Exit(1)
	}
	defer f.Close()

	start = time.Now()
	for k, v := range common.Account_SlotHash2Slot {

		s := fmt.Sprintln("/acc:", k)
		for _, vv := range v {

			// blocknumber, _ := strconv.ParseInt(common.Bytes2Hex(vv[3]), 16, 64)
			// s += fmt.Sprintln("  blocknumber", string(vv[3]), "slothash", common.BytesToHash(vv[0]), "slot", common.BytesToAddress(vv[1]), "value", common.Bytes2Hex(vv[2]))
			s += fmt.Sprintln("  blocknumber", vv.Blocknumber, "slothash", vv.Hashkey, "slot", vv.Key, "value", common.Bytes2Hex(vv.Value))
		}

		fmt.Fprintln(f, s)
	}

	elapsed = time.Since(start)
	ss += fmt.Sprintln(" in", elapsed.Seconds(), "seconds")
	fmt.Print(ss)

	common.Account_SlotHash2Slot = map[common.Address][]common.SlotDetail{}

}

func PrintTxDetail(blocknumber, distance int) {
	common.GlobalMutex.Lock()
	defer common.GlobalMutex.Unlock()

	count := 0
	c := 0
	gap := len(common.TxDetail) / 4

	ss := fmt.Sprintf("Write TxDetail %d-%d in txt file", blocknumber-distance, blocknumber)
	filepath := path + "TxDetail_" + strconv.FormatInt(int64(blocknumber-distance+1), 10) + "-" + strconv.FormatInt(int64(blocknumber), 10) + ".txt"

	f, err := os.Create(filepath)
	if err != nil {
		fmt.Printf("Cannot create result file.\n")
		os.Exit(1)
	}
	defer f.Close()

	start := time.Now()
	for k, v := range common.TxDetail {
		s := ""

		count++
		if gap != 0 {
			if count%gap == 0 {
				c++
				elapsed := time.Since(start)
				fmt.Println("Writing TxDetail...[", c, "/4]", "elapsed", elapsed.Seconds(), "seconds")

			}
		}

		if v.Types != 1 { //except transfer transaction

			s += fmt.Sprintln("/TxID:", k)
			s += fmt.Sprintln("  Block:", v.BlockNumber)
			if v.Types == 1 {
				s += fmt.Sprintln("  Tx: Transfer")
				// s += fmt.Sprintln("  common.TxInformation: Transfer or Contract call")
			} else if v.Types == 2 {
				s += fmt.Sprintln("  Tx: Contract creation tx")
			} else if v.Types == 3 {
				s += fmt.Sprintln("  Tx: Contract call")
			} else if v.Types == 4 {
				s += fmt.Sprintln("  Tx: Failed Transaction")
			} else {
				s += fmt.Sprintln("  Wrong Tx Information")
			}

			s += fmt.Sprintln("    From:", v.From, "Bal:", v.AccountBalance[v.From])
			if v.Types == 1 {
				s += fmt.Sprintln("    To(EOA):", v.To, "Bal:", v.AccountBalance[v.To])
				// s += fmt.Sprintln("    To: \t\t", v.To)
			} else if v.Types == 3 {
				s += fmt.Sprintln("    To(CA):", v.To, "Bal:", v.AccountBalance[v.To])
			} else if v.Types == 4 {
				// Do nothing
			} else {
				s += fmt.Sprintln("    DeployedCA:", v.DeployedContractAddress)
			}

			if v.Types != 1 && v.Types != 4 {
				s += fmt.Sprintln("    Contract related Address")
				for _, vv := range v.Internal {
					s += fmt.Sprintln("      EOA:", vv, "Bal:", v.AccountBalance[vv]) // EOAs balance modified by contract
				}
				for _, vv := range v.InternalCA {
					s += fmt.Sprintln("      CAs:", vv, "Bal:", v.AccountBalance[vv]) // CAs balance modified by contract
				}

				for kk, vv := range v.ContractAddress_SlotHash {

					s += fmt.Sprintln("      CA:", kk, "Bal:", v.AccountBalance[kk])
					s += fmt.Sprintln("        SlotHash:", vv)
				}
			}
			// s += fmt.Sprintln()
			fmt.Fprintln(f, s)
		}

	}

	elapsed := time.Since(start)
	ss += fmt.Sprintln(" in", elapsed.Seconds(), "seconds")
	fmt.Print(ss)

	common.TxDetail = map[common.Hash]*common.TxInformation{}

}

func PrintTxElse(blocknumber, distance int) {
	common.GlobalMutex.Lock()
	defer common.GlobalMutex.Unlock()

	ss := fmt.Sprintf("Write TxElse %d-%d in txt file", blocknumber-distance, blocknumber)
	filepath := path + "TrieUpdateElse_" + strconv.FormatInt(int64(blocknumber-distance+1), 10) + "-" + strconv.FormatInt(int64(blocknumber), 10) + ".txt"
	f, err := os.Create(filepath)
	if err != nil {
		fmt.Printf("Cannot create result file.\n")
		os.Exit(1)
	}
	defer f.Close()

	start := time.Now()
	for k, v := range common.TrieUpdateElse {

		if len(v) != 0 {
			s := ""
			s += fmt.Sprintf("%v,", k)
			for _, val := range v {
				s += fmt.Sprintf("%v,", val)
			}
			// s += fmt.Sprintln("")
			if s != "" {
				fmt.Fprintln(f, s)
			}
		}

	}

	fmt.Fprintln(f, "Miner and uncles")
	for k, v := range common.MinerUnlce {
		s := ""
		if len(v) != 0 {
			s += fmt.Sprintf("%d,%v", k, v)
		}
		fmt.Fprintln(f, s)
	}
	common.MinerUnlce = map[int]map[common.Address]uint64{}
	if blocknumber-distance == 0 { // write only first file
		fmt.Fprintln(f, "Block 0 state")
		for k, v := range common.ZeroBlockTI.AccountBalance {
			s := fmt.Sprintf("%v,%v", k, v)
			fmt.Fprintln(f, s)
		}
	}

	elapsed := time.Since(start)
	ss += fmt.Sprintln(" in", elapsed.Seconds(), "seconds")
	fmt.Print(ss)

	common.TrieUpdateElse = map[int][]common.Address{}
}

func PrintDuplicatedFlushedNode(blocknumber, distance int) {
	common.GlobalMutex.Lock()
	defer common.GlobalMutex.Unlock()

	ss := fmt.Sprintf("Write DuplicateFlushedNode %d-%d in txt file", blocknumber-distance, blocknumber)
	filepath := path + "DuplicateFlushedNode_" + strconv.FormatInt(int64(blocknumber-distance+1), 10) + "-" + strconv.FormatInt(int64(blocknumber), 10) + ".txt"

	f, err := os.Create(filepath)
	if err != nil {
		fmt.Printf("Cannot create result file.\n")
		os.Exit(1)
	}
	defer f.Close()

	start := time.Now()
	// pretty print for flushed duplicate node
	for k, v := range common.FlushedNodeDuplicate_block {
		s := ""
		if len(v) != 1 {

			s += fmt.Sprintf("%v,%d,%v", k, len(v), v)
			fmt.Fprintln(f, s)
		}

	}

	elapsed := time.Since(start)
	ss += fmt.Sprintln(" in", elapsed.Seconds(), "seconds")
	fmt.Print(ss)

	common.FlushedNodeDuplicate_block = map[common.Hash][]int{}
	common.FlushedNodeDuplicate = map[common.Hash]int{}

}

// Trie size inspection from nakamoto.snu.ac.kr(jhkim)

var (
	cnt          = 0
	rwMutex      = new(sync.RWMutex)
	maxGoroutine = 10000
	wg           sync.WaitGroup
	isFirst      = true
)

type KV struct {
	Key   string
	Value int
}

// trie inspecting results (jhkim)
type TrieInspectResult struct {
	Count           int // number of calling function TriInspectNode
	TrieSize        int // bytes
	LeafNodeNum     int // # of leaf nodes in the trie (if this trie is state trie, then = EOANum + CANum)
	LeafNodeSize    int
	LeafNodeEOASize int
	LeafNodeCASize  int
	EOANum          int // # of Externally Owned Accounts in the trie
	CANum           int // # of Contract Accounts in the trie
	FullNodeNum     int // # of full node in the trie
	FullNodeSize    int
	ShortNodeNum    int // # of short node in the trie
	ShortNodeSize   int
	EmptyRootHash   int
	ErrorNum        int                // # of error occurence while inspecting the trie
	AccountBalance  map[string]big.Int // key: addressHash, value: account balance

	StateTrieFullNodeDepth  [20]int
	StateTrieShortNodeDepth [20]int
	StateTrieLeafNodeDepth  [20]int

	StorageTrieNum           int // # of non-empty storage tries (for state trie inspection)
	StorageTrieSizeSum       int // total size of storage tries (for state trie inspection)
	StorageTrieLeafNodeNum   int // # of nodes in all storage trie
	StorageTrieLeafNodeSize  int
	StorageTrieFullNodeNum   int
	StorageTrieFullNodeSize  int
	StorageTrieShortNodeNum  int
	StorageTrieShortNodeSize int

	StorageTrieLeafNodeDepth map[string]([20]int) // map Key:codeHash, value:leafnode depth. 20 is hard coded number. Depth of Trie will not exceed 20
	StorageTrieNodeMap       map[string]([6]int)  // map Key:codeHash, value: [shortnode count, size, fullnode count, size, leafnode count, size]
	AddressHashTocodeHash    map[string]string    // In state trie, map Key: addressHash, value: codeHash
	StorageTrieSlotHash      map[string][]string  // Storage Trie SlotHash, key: addressHash, value: slotHash List
	StorageTrieRoot          map[string]string    // map Key: storageTrie Root Hash, value: addressHash

}

func (tir *TrieInspectResult) PrintTrieInspectResult(blockNumber uint64, elapsedTime int) {
	// f1, err := os.Create("/home/jhkim/go/src/github.com/ethereum/go-ethereum/build/bin/new_result_" + strconv.FormatUint(blockNumber, 10) + ".txt") // goroutine version
	// f1, err := os.Create("/home/jhkim/go/src/github.com/ethereum/new_result_" + strconv.FormatUint(blockNumber, 10) + "_vanilla.txt") // vanilla version
	f1, err := os.Create("./trie_inspect_result.txt") // vanilla version

	if err != nil {
		fmt.Printf("Cannot create result file.\n")
		os.Exit(1)
	}
	defer f1.Close()

	output := ""
	// output += fmt.Sprintf("trie inspect result at block %d with %d goroutines (it took %d seconds)\n", blockNumber, maxGoroutine, elapsedTime)
	output += fmt.Sprintf("trie inspect result at block %d with vanilla goroutines (it took %d seconds)\n", blockNumber, elapsedTime) // vanilla version
	output += fmt.Sprintf("  total trie size: %d bytes (about %d MB)\n", tir.TrieSize, tir.TrieSize/1000000)
	output += fmt.Sprintf("  # of full nodes: %d\n", tir.FullNodeNum)
	output += fmt.Sprintf("  \ttotal size of full nodes: %d\n", tir.FullNodeSize)
	output += fmt.Sprintf("  # of short nodes: %d\n", tir.ShortNodeNum)
	output += fmt.Sprintf("  \ttotal size of short nodes: %d\n", tir.ShortNodeSize)
	// output += fmt.Sprintf("  # of intermediate nodes: %d\n", tir.IntermediateNodeNum)
	output += fmt.Sprintf("  # of leaf nodes: %d ( EOA: %d / CA: %d )\n", tir.LeafNodeNum, tir.EOANum, tir.CANum)
	output += fmt.Sprintf("  \ttotal size of leaf nodes: %d( EOA: %d / CA: %d )\n", tir.LeafNodeSize, tir.LeafNodeEOASize, tir.LeafNodeCASize)

	output += fmt.Sprintf("  depth distribution of Full nodes: %d\n", tir.StateTrieFullNodeDepth)
	output += fmt.Sprintf("  depth distribution of Short nodes: %d\n", tir.StateTrieShortNodeDepth)
	output += fmt.Sprintf("  depth distribution of Leaf nodes: %d\n", tir.StateTrieLeafNodeDepth)
	output += fmt.Sprintln("")

	output += fmt.Sprintf("  # of empty root hash: %d\n", tir.EmptyRootHash)
	output += fmt.Sprintf("  # of non-empty storage tries: %d ( %d bytes = %d MB )\n", tir.StorageTrieNum, tir.StorageTrieSizeSum, tir.StorageTrieSizeSum/1000000)
	output += fmt.Sprintf("  # of Full nodes of storage tries: %d\n", tir.StorageTrieFullNodeNum)
	output += fmt.Sprintf("  \ttotal size of Full nodes storage tries: %d\n", tir.StorageTrieFullNodeSize)
	output += fmt.Sprintf("  # of short nodes of storage tries: %d\n", tir.StorageTrieShortNodeNum)
	output += fmt.Sprintf("  \ttotal size of Short nodes storage tries: %d\n", tir.StorageTrieShortNodeSize)
	output += fmt.Sprintf("  # of leaf nodes of storage tries: %d\n", tir.StorageTrieLeafNodeNum)
	output += fmt.Sprintf("  \ttotal size of Leaf nodes storage tries: %d\n", tir.StorageTrieLeafNodeSize)

	output += fmt.Sprintf("  # of errors: %d\n", tir.ErrorNum) // this should be 0, of course

	fmt.Fprintln(f1, output) // write text file
	// fmt.Println(output)      // console print

	// output = fmt.Sprintf("Balance of Accounts\n")
	// for key, value := range tir.AccountBalance {
	// 	output += fmt.Sprintf("  %s,%v\n", key, value.Uint64())
	// }
	// fmt.Fprintln(f1, output) // write text file
	fmt.Println(output) // console print

	// list all StorageTrie's codehash, leafnode depth, each node size
	output = fmt.Sprintf("\n\nNonZero StorageTrie Leaf Node Depth: addressHash, codeHash, leaf node depth, [SN #, size, FN #, size, LN #, size]\n")
	for key, value := range tir.StorageTrieLeafNodeDepth {
		output += fmt.Sprintf("\n  %s,%v,%v,%v\n", key, tir.AddressHashTocodeHash[key], value, tir.StorageTrieNodeMap[key])
		output += fmt.Sprintf("    StorageTrieRoot: %v\n", tir.StorageTrieRoot[key])
		output += fmt.Sprintf("    SlotHash: %v\n", tir.StorageTrieSlotHash[key])
		// for _, v := range tir.StorageTrieSlotHash[key] {
		// 	output += fmt.Sprintf("      %v\n", v)
		// }
}

// convert trie.TrieInspectResult to common.NodeStat (jmlee)
func (tir *TrieInspectResult) ToNodeStat() common.NodeStat {

	// key for trie node: hash -> 32B
	keySize := uint64(32)

	// convert trie.TrieInspectResult to common.NodeStat
	var stateNodeStat common.NodeStat
	stateNodeStat.FullNodesNum = uint64(tir.FullNodeNum)
	stateNodeStat.FullNodesSize = uint64(tir.FullNodeSize) + keySize*stateNodeStat.FullNodesNum
	stateNodeStat.ShortNodesNum = uint64(tir.ShortNodeNum)
	stateNodeStat.ShortNodesSize = uint64(tir.ShortNodeSize) + keySize*stateNodeStat.ShortNodesNum
	stateNodeStat.LeafNodesNum = uint64(tir.LeafNodeNum)
	stateNodeStat.LeafNodesSize = uint64(tir.LeafNodeSize) + keySize*stateNodeStat.LeafNodesNum

	// deal with depths
	var avgDepth float64
	minDepth := 100
	maxDepth := 0
	depthSum := 0
	for depth, num := range tir.StateTrieLeafNodeDepth {
		depthSum += depth * num
		if maxDepth < depth && num != 0 {
			maxDepth = depth
		}
		if minDepth > depth && num != 0 {
			minDepth = depth
		}
	}
	if stateNodeStat.LeafNodesNum != 0 {
		avgDepth = float64(depthSum) / float64(stateNodeStat.LeafNodesNum)
	}

	// print state trie inspect result
	fmt.Println("convert state trie inspect result to nodeStat")
	fmt.Println("avg depth:", avgDepth, "( min:", minDepth, "/ max:", maxDepth, ")")
	stateNodeStat.Print()
	fmt.Println(stateNodeStat.ToString(" "), minDepth, avgDepth, maxDepth)

	// convert trie.TrieInspectResult to common.NodeStat
	var storageNodeStat common.NodeStat
	storageNodeStat.FullNodesNum = uint64(tir.StorageTrieFullNodeNum)
	storageNodeStat.FullNodesSize = uint64(tir.StorageTrieFullNodeSize) + keySize*storageNodeStat.FullNodesNum
	storageNodeStat.ShortNodesNum = uint64(tir.StorageTrieShortNodeNum)
	storageNodeStat.ShortNodesSize = uint64(tir.StorageTrieShortNodeSize) + keySize*storageNodeStat.ShortNodesNum
	storageNodeStat.LeafNodesNum = uint64(tir.StorageTrieLeafNodeNum)
	storageNodeStat.LeafNodesSize = uint64(tir.StorageTrieLeafNodeSize) + keySize*storageNodeStat.LeafNodesNum

	// deal with depths
	minDepth = 100
	maxDepth = 0
	depthSum = 0
	for _, depths := range tir.StorageTrieLeafNodeDepth {
		for depth, num := range depths {
			depthSum += depth * num
			if maxDepth < depth && num != 0 {
				maxDepth = depth
			}
			if minDepth > depth && num != 0 {
				minDepth = depth
			}
		}
	}
	if storageNodeStat.LeafNodesNum != 0 {
		avgDepth = float64(depthSum) / float64(storageNodeStat.LeafNodesNum)
	} else {
		avgDepth = 0
		minDepth = 0
	}

	// print storage trie inspect result
	fmt.Println("convert storage trie inspect result to nodeStat -> storage tries num:", tir.StorageTrieNum)
	fmt.Println("avg depth:", avgDepth, "( min:", minDepth, "/ max:", maxDepth, ")")
	storageNodeStat.Print()
	fmt.Println(storageNodeStat.ToString(" "), minDepth, avgDepth, maxDepth)

	return stateNodeStat
}

// type Account struct {
// 	Nonce    uint64
// 	Balance  *big.Int
// 	Root     common.Hash // merkle root of the storage trie
// 	CodeHash []byte
// }

type trieType int

const stateTrie trieType = 1
const storageTrie trieType = 2

// inspect the trie
// caution: inspect trie stats can be larger than real, since these might include duplicated nodes
func (t *Trie) InspectTrie() TrieInspectResult {
	// if isFirst {

	// 	isFirst = false
	// 	debug.SetMaxThreads(15000) // default MaxThread is 10000
	// 	runtime.GOMAXPROCS(runtime.NumCPU())
	// }
	// debug.FreeOSMemory()
	var tir TrieInspectResult
	// _, _ = os.Create(filename)
	tir.StorageTrieLeafNodeDepth = map[string][20]int{}
	tir.StorageTrieNodeMap = map[string][6]int{}
	tir.AddressHashTocodeHash = map[string]string{}
	tir.StorageTrieSlotHash = map[string][]string{}
	tir.StorageTrieRoot = map[string]string{}
	tir.AccountBalance = map[string]big.Int{}

	// fmt.Println("\n\n#################\nINSPECTATION START\n#################")
	// t.inspectTrieNodes(t.root, &tir, &wg, 0, "state") // Inspect Start
	t.inspectTrieNodes(t.root, &tir, &wg, 0, stateTrie, nil) // Inspect Start

	// wait for all gourtines finished
	// timeout := 48 * time.Hour

	// if waitTimeout(&wg, timeout) {
	// 	fmt.Println("Timed out waiting for wait group")
	// } else {
	// 	fmt.Println("Wait group finished")
	// }
	wg.Wait() // ***WARNING: if you sets maxgoroutine number wrong, infinite waiting
	// fmt.Println("\n\n#################\nINSPECTATION DONE\n#################")
	// tir.ToNodeStat()
	return tir
}

func (t *Trie) InspectStorageTrie() TrieInspectResult {

	var tir TrieInspectResult

	tir.StorageTrieLeafNodeDepth = map[string][20]int{}
	tir.StorageTrieNodeMap = map[string][6]int{}
	tir.AddressHashTocodeHash = map[string]string{}
	tir.StorageTrieSlotHash = map[string][]string{}

	t.inspectTrieNodes(t.root, &tir, &wg, 0, storageTrie, nil)
	// t.inspectTrieNodes(t.root, &tir, &wg, 0, "storage")
	return tir
}

func (t *Trie) inspectTrieNodes(n node, tir *TrieInspectResult, wg *sync.WaitGroup, depth int, trie trieType, addressKey []byte) {
	// func (t *Trie) inspectTrieNodes(n node, tir *TrieInspectResult, wg *sync.WaitGroup, depth int, trie string) {

	cnt += 1
	if cnt%100000 == 0 && trie == stateTrie {
		cnt = 0
		fmt.Println("  intermediate result -> trie size:", tir.TrieSize/1000000, "MB / goroutines", runtime.NumGoroutine(), "/ EOA:", tir.EOANum, "/ CA:", tir.CANum, "/ time:", time.Now().Format("01-02 3:04:05"), "/ err:", tir.ErrorNum)
	}

	switch n := n.(type) {
	case *shortNode:

		hn, _ := n.cache()
		if hn == nil { // storage trie case
			// this node is smaller than 32 bytes, cause there is no cached hash
			// "Nodes smaller than 32 bytes are stored inside their parent"
			// this node's size is already counted in the parent node, so just return
			return
		}
		// hash := common.BytesToHash(hn)
		addressKey = append(addressKey, n.Key...)

		// encoding node to measure node size (in trie/hasher.go -> hashShortNodeChildren(), shortnodeToHash())
		h := newHasher(false)
		defer returnHasherToPool(h)
		collapsed, _ := n.copy(), n.copy()
		collapsed.Key = hexToCompact(n.Key)
		collapsed.Val, _ = h.hash(n.Val, false)
		_, nodeSize := h.shortnodeToHash(collapsed, false)

		// increase tir
		if n.Key[len(n.Key)-1] != 16 {
			// this is intermediate short node
			increaseSize(nodeSize, "short", tir, depth)
		} else {
			// this is leaf short node
			increaseSize(nodeSize, "value", tir, depth)
		}

		t.inspectTrieNodes(n.Val, tir, wg, depth+1, trie, addressKey) // go child node

	case *fullNode:

		hn, _ := n.cache()
		if hn == nil { // storage trie case
			// this node is smaller than 32 bytes, cause there is no cached hash
			// "Nodes smaller than 32 bytes are stored inside their parent"
			// this node's size is already counted in the parent node, so just return
			return
		}
		// hash := common.BytesToHash(hn)

		// encoding node to measure node size (in trie/hasher.go -> hashFullNodeChildren(), fullnodeToHash())
		h := newHasher(false)
		defer returnHasherToPool(h)
		collapsed, _ := n.copy(), n.copy()
		for i := 0; i < 16; i++ {
			if child := n.Children[i]; child != nil {
				collapsed.Children[i], _ = h.hash(child, false)
			} else {
				collapsed.Children[i] = nilValueNode
			}
		}
		_, nodeSize := h.fullnodeToHash(collapsed, false)

		temp_addressKey := addressKey
		increaseSize(nodeSize, "full", tir, depth) // increase tir

		// // vanilla version
		for i, child := range &n.Children {
			if child != nil {
				nodeKeyByte := common.HexToHash("0x" + indices[i])
				// fmt.Println("child of fullNode", "index", i, "nodekeyByte", nodeKeyByte)
				// fmt.Println("    Full  node: addressKey", hex.EncodeToString(addressKey), "+", nodeKeyByte[len(nodeKeyByte)-1])
				addressKey = append(temp_addressKey, nodeKeyByte[len(nodeKeyByte)-1])

				t.inspectTrieNodes(child, tir, wg, depth+1, trie, addressKey)
				// t.inspectTrieNodes(child, tir, wg, depth+1, trie)
			}
		}

		// goroutine version
		// gortn := runtime.NumGoroutine()
		// if gortn < maxGoroutine && trie == "state" { // if current number of goroutines exceed max goroutine number
		// 	for i, child := range &n.Children {
		// 		if child != nil {
		// 			nodeKeyByte := common.HexToHash("0x" + indices[i])
		// 			addressKey = append(addressKey, nodeKeyByte[len(nodeKeyByte)-1])

		// 			wg.Add(1)
		// 			go func(child node, tir *TrieInspectResult, wg *sync.WaitGroup, depth int, trie string) {
		// 				defer wg.Done()
		// 				t.inspectTrieNodes(child, tir, wg, depth+1, trie, addressKey)
		// 				// t.inspectTrieNodes(child, tir, wg, depth+1, trie)
		// 			}(child, tir, wg, depth, trie)
		// 		}
		// 	}
		// } else {
		// 	for i, child := range &n.Children {
		// 		if child != nil {
		// 			// t.inspectTrieNodes(child, tir, wg, depth+1, trie)
		// 			nodeKeyByte := common.HexToHash("0x" + indices[i])
		// 			addressKey = append(addressKey, nodeKeyByte[len(nodeKeyByte)-1])

		// 			t.inspectTrieNodes(child, tir, wg, depth+1, trie, addressKey)
		// 		}
		// 	}
		// }

	case hashNode:
		hash := common.BytesToHash([]byte(n))
		resolvedNode := t.db.node(hash) // error
		if resolvedNode != nil {
			// t.inspectTrieNodes(resolvedNode, tir, wg, depth, trie)
			t.inspectTrieNodes(resolvedNode, tir, wg, depth, trie, addressKey)

		} else {
			// in normal case (ex. archive node), it will not come in here
			fmt.Println("ERROR: cannot resolve hash node -> node hash:", hash.Hex())
			os.Exit(1)
		}

	case valueNode:

		// Value nodes don't have children so they're left as were

		// value node has account info, decode it
		addressHash := common.BytesToHash(hexToKeybytes(addressKey)).Hex()
		// fmt.Printf("	inspectTrieNodes, addressKey %v ->", hex.EncodeToString(addressKey))
		// fmt.Printf("this node is value node (size: %d bytes) ", len(n))
		// fmt.Println("addressHash:", addressHash)

		// decode value node to check this is storage trie or not
		var acc types.StateAccount
		var ethaneAcc types.EthaneStateAccount
		isStorageTrie := false
		if common.IsEthane {
			err := rlp.DecodeBytes(n, &ethaneAcc)
			if err != nil {
				isStorageTrie = true
			} else {
				acc.Balance = ethaneAcc.Balance
				acc.Nonce = ethaneAcc.Nonce
				acc.CodeHash = ethaneAcc.CodeHash
				acc.Root = ethaneAcc.Root
			}
		} else {
			err := rlp.DecodeBytes(n, &acc)
			if err != nil {
				isStorageTrie = true
			}
		}

		if isStorageTrie {
			// valuenode is not an account, which means inspectig trie is "storage Trie"
			tir.AddressHashTocodeHash[addressHash] = "" // In this case, addressHash means slotHash of storage Trie and codeHash is empty

		} else {
			// valuenode is an account, which means inspectig trie is "state Trie"
			// check if account has empty codeHash value or not
			codeHash := common.Bytes2Hex(acc.CodeHash)
			tir.AddressHashTocodeHash[addressHash] = codeHash

			if codeHash == "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470" { // empty code hash. This account is EOA
				rwMutex.Lock()
				tir.EOANum += 1
				tir.LeafNodeEOASize += len(n)
				tir.AccountBalance[addressHash] = *acc.Balance
				rwMutex.Unlock()
			} else {
				rwMutex.Lock()
				tir.CANum += 1
				tir.LeafNodeCASize += len(n)
				tir.AccountBalance[addressHash] = *acc.Balance
				rwMutex.Unlock()

				// inspect CA's storage trie (if it is not empty trie)
				if acc.Root.Hex() != "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421" { // empty root hash
					storageTrie, err := NewSecure(acc.Root, t.db) // storage trie is secure trie
					if err != nil {
						fmt.Println("ERROR: cannot find the storage trie")
						rwMutex.Lock()
						tir.ErrorNum += 1
						rwMutex.Unlock()
					} else {
						tir.StorageTrieNum += 1
						storageTrieRootHash := storageTrie.Hash()
						if acc.Root.Hex() != storageTrieRootHash.Hex() {
							fmt.Println("maybe this is problem")
							fmt.Println("saved storage root:", acc.Root.Hex(), "/ rehashed storage root:", storageTrieRootHash.Hex())
						}

						// storage trie inspect
						storageTir := storageTrie.InspectStorageTrie()
						// storageTir.PrintTrieInspectResult()
						tir.StorageTrieRoot[addressHash] = storageTrieRootHash.Hex()
						rwMutex.Lock()

						tir.StorageTrieSizeSum += storageTir.TrieSize
						tir.ErrorNum += storageTir.ErrorNum

						tir.StorageTrieFullNodeNum += storageTir.FullNodeNum
						tir.StorageTrieFullNodeSize += storageTir.FullNodeSize
						tir.StorageTrieShortNodeNum += storageTir.ShortNodeNum
						tir.StorageTrieShortNodeSize += storageTir.ShortNodeSize
						tir.StorageTrieLeafNodeNum += storageTir.LeafNodeNum
						tir.StorageTrieLeafNodeSize += storageTir.LeafNodeSize

						// if you want to see storage trie's node distribution, add fields of storageTrie

						tir.StorageTrieLeafNodeDepth[addressHash] = storageTir.StateTrieLeafNodeDepth
						// fmt.Println("@@@@@", storageTir.AddressHashTocodeHash)
						tmpSlice := []string{}
						for k := range storageTir.AddressHashTocodeHash {
							tmpSlice = append(tmpSlice, k)
						}
						tir.StorageTrieSlotHash[addressHash] = tmpSlice
						// fmt.Println("StorageTir addresshash to slotHash: ", addressHash, tir.StorageTrieSlotHash[addressHash])

						tir.StorageTrieNodeMap[addressHash] = [6]int{
							storageTir.FullNodeNum,
							storageTir.FullNodeSize,
							storageTir.ShortNodeNum,
							storageTir.ShortNodeSize,
							storageTir.LeafNodeNum,
							storageTir.LeafNodeSize,
						}

						rwMutex.Unlock()

						if storageTir.ErrorNum != 0 {
							fmt.Print("!!! ERROR: something is wrong while inspecting storage trie ->", storageTir.ErrorNum, "errors\n\n")
							// os.Exit(1)
						}
					}
				} else {
					tir.EmptyRootHash += 1
				}

			}
		}
	default:
		// should not reach here! maybe there is something wrong
		fmt.Println("ERROR: unknown trie node type? node:", n)
		os.Exit(1)
	}
}

// deprecated functions

// choose top N big Storage Trie and return map (jhkim)
func findTopNStorageTrie(StorageTrieNodeMap map[string]([7]int), top_number int) map[string]int {
	topN_map := map[string]int{}
	min_size := 0
	min_codehash := ""
	// min_codehash := "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"

	for codehash, value := range StorageTrieNodeMap {
		sizeSum := value[1] + value[3] + value[5] // fullnodesize + shortnodesize + leafnodesize

		if codehash == "" {
			fmt.Println("nil codehash in StorageTrieNodeMap! 1. empty storage Trie 2. Error")
			break

		} else if len(topN_map) < top_number {
			topN_map[codehash] = sizeSum

			if min_size == 0 || sizeSum < min_size {
				min_size = sizeSum
				min_codehash = codehash
			}

		} else if sizeSum <= min_size {
			continue

		} else { // change topN_map
			delete(topN_map, min_codehash)
			min_size = sizeSum
			min_codehash = codehash

			for key, value := range topN_map {
				if value < min_size {
					min_size = value
					min_codehash = key
				}
			}
			topN_map[codehash] = sizeSum
		}
	}

	return topN_map
}

// waitTimeout waits for the waitgroup for the specified max timeout. (jhkim)
// Returns true if waiting timed out.
func waitTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	c := make(chan struct{})
	go func() {
		defer close(c)
		wg.Wait()
	}()
	select {
	case <-c:
		return false // completed normally
	case <-time.After(timeout):
		return true // timed out
	}
}

// Descending sort map by value (jhkim)
func sortByValue(m map[string]int) []KV {
	var ss []KV
	for k, v := range m {
		ss = append(ss, KV{k, v})
	}

	sort.Slice(ss, func(i, j int) bool {
		return ss[i].Value > ss[j].Value
	})

	return ss
}
