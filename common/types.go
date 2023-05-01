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

package common

import (
	"bytes"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"golang.org/x/crypto/sha3"
)

// Lengths of hashes and addresses in bytes.
const (
	// HashLength is the expected length of the hash
	HashLength = 32
	// AddressLength is the expected length of the address
	AddressLength = 20
)

var (
	hashT    = reflect.TypeOf(Hash{})
	addressT = reflect.TypeOf(Address{})

	//
	// general options
	//

	// option: simulation mode (0: original ethereum, 1: Ethane, 2: Ethanos)
	SimulationMode = 0

	// option: trie flush interval (default: 1 -> flush state tries at every block / but genesis block is always flushed)
	FlushInterval = uint64(1)

	// option: don't stop Ethereum simulation when storage trie is different from Geth's
	AcceptWrongStorageTrie = false

	// option: collect flushed node infos or not
	// (if CollectNodeInfos is false, then several functionalities will be deprecated, ex. inspectTrieMem())
	CollectNodeInfos = false

	// option: collect flushed node hashes or not
	// (if CollectNodeInfos is true, then TrieNodeHashes is empty, since TrieNodeInfos will collect hashes)
	// (if CollectNodeHahes, CollectNodeInfos are both false, then TotalNodeStat, TotalStorageNodeStat might be wrong)
	CollectNodeHashes = false

	// option: max number of blocks to store in "Blocks" (i.e., rollback limit)
	MaxBlocksToStore = uint64(100000000)

	//
	// db stats for trie nodes (jmlee)
	//

	// TrieNodeInfos[nodeHash] = node's information (containing all nodes flushed to diskdb)
	TrieNodeInfos = make(map[Hash]NodeInfo)
	// infos for dirty trie nodes, that will be flushed or discarded
	TrieNodeInfosDirty = make(map[Hash]NodeInfo)
	// all flushed nodes' hashes
	TrieNodeHashes = make(map[Hash]struct{})

	// TODO(jmlee): change name to TotalStateNodeStat
	// stats for newly flushed state trie's nodes in latest block
	NewNodeStat NodeStat
	// stats for all state trie's nodes in db (archive data)
	TotalNodeStat NodeStat
	// this is needed to split flush results
	FlushStorageTries bool
	// stats for newly flushed storage tries' nodes in latest block
	NewStorageNodeStat NodeStat
	// stats for all storage tries' nodes in db (archive data)
	TotalStorageNodeStat NodeStat
	// this is needed to split flush results
	FlushInactiveTrie bool
	// stats for newly flushed inactive trie's nodes in latest block
	NewInactiveNodeStat NodeStat
	// stats for inactive trie's nodes in db (archive data)
	TotalInactiveNodeStat NodeStat

	// mutex to avoid fatal error: "concurrent map read and map write"
	ChildHashesMutex = sync.RWMutex{}

	// Blocks[blockNum] = block's information
	Blocks = make(map[uint64]BlockInfo)
	// next block number to generate (which will be increased after flushing trie nodes)
	// latest flushed block num = next block num - 1 (if next block num == 0, then no block exists)
	NextBlockNum = uint64(0)

	// to convert trie to graph representation
	TrieGraph TrieGraphInfo
)

var (

	//
	// Ethane options
	//

	// option: active trie & inactive trie vs one state trie
	InactiveTrieExist = true
	// option: do light deletion
	// for Ethane's light inactive trie delete (jmlee)
	DoLightInactiveDeletion = true
	// this is not an option, but a flag
	// for Ethane's light inactive trie delete (jmlee)
	DeletingInactiveTrieFlag = false
	// adopt new inactivation method which is faster than the old method
	// but must set this false when DeleteEpoch = INF && InactivateEpoch != INF
	// (or add additional feature in the new method)
	DoNewInactivation = true

	// TODO(jmlee): implement this
	// option: forcely stop simulation when error occurs or not
	StopWhenErrorOccurs = false

	DeleteEpoch         = uint64(3) // option: block epoch to delete previous leaf nodes (from active area to temp area)
	InactivateEpoch     = uint64(3) // option: block epoch to inactivate inactive leaf nodes (from temp area to inactive trie)
	InactivateCriterion = uint64(3) // option (also for Ethanos): inactive accounts were touched more before than this block timestamp (min: 1) (const)

	// option (also for Ethanos): omit some parent nodes in Merkle proofs (ex. 1: omit root node)
	FromLevel = uint(0)

	//
	// vars for Ethane simulation (jmlee)
	//

	// temp map for verifying compactTrie idea(jmlee)
	// map storing active address list (AddrToKeyActive[addr] = counter key in trie's active part)
	AddrToKeyActive = make(map[Address]Hash)
	// map storing inactive address list (AddrToKeyActive[addr] = counter keys in trie's inactive part)
	AddrToKeyInactive = make(map[Address][]Hash)
	// to avoid fatal error: "concurrent map read and map write"
	AddrToKeyMapMutex = sync.RWMutex{}
	// disk path to save AddrToKey (will be set as [datadir]/geth/chaindata/) (const)
	// AddrToKeyPath = ""
	// restored addresses among active addresses
	RestoredAddresses = make(map[Address]struct{})
	// crumb addresses among active addresses
	CrumbAddresses = make(map[Address]struct{})

	NextKey        = uint64(0)               // key of the first 'will be inserted' account
	CheckpointKey  = uint64(0)               // save initial NextKey value to determine whether move leaf nodes or not
	CheckpointKeys = make(map[uint64]uint64) // initial NextKeys of blocks (CheckpointKeys[blockNumber] = initialNextKeyOfTheBlock)

	KeysToDelete         = make([]Hash, 0)         // store previous leaf nodes' keys to delete later
	KeysToDeleteMap      = make(map[Hash]struct{}) // store previous leaf nodes' keys to delete later (need to inactivate accounts when delete epoch is infinite)
	InactiveBoundaryKey  = uint64(0)               // inactive accounts have keys smaller than InactiveBoundaryKey
	InactiveBoundaryKeys = make(map[uint64]uint64) // InactiveBoundaryKeys[blockNum] = inactiveBoundaryKey at that block (TODO(jmlee): maybe merge into BlockInfo)
	RestoredKeys         = make([]Hash, 0)         // merkle proof keys in restore txs, need to be deleted after inactivation

	DeletedActiveNodeNum   = uint64(0) // total deleted leaf nodes in active trie
	DeletedInactiveNodeNum = uint64(0) // total deleted leaf nodes in inactive trie (# of used Merkle proofs for restoration)

	// TODO(jmlee): logging restore related stats for current block
	BlockRestoreStat RestoreStat

	// how many corner cases occurred in inactive trie
	InactiveCornerCaseNum  = uint64(0)
	LastCornerCaseBlockNum = uint64(0)

	// how many zero hash nodes are needed in inactive trie
	ZeroHashNodeNum        = uint64(0)
	DeletedZeroHashNodeNum = uint64(0) // deleted zero hash nodes with full node

	// very large key which will not be reached forever
	NoExistKey     = HexToHash("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	ToBeDeletedKey = HexToHash("0xfffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffe")
	ZeroAddress    = HexToAddress("0x0")

	// this means infinite epoch for deletion & inactivation
	InfiniteEpoch = uint64(100000000)
)

// NodeInfo stores trie node related information
type NodeInfo struct {
	Size uint // size of node (i.e., len of rlp-encoded node)

	ChildHashes []Hash // hashes of child nodes
	IsShortNode bool   // type of node (short node vs full node)
	IsLeafNode  bool
	Indices     []string // full node's indices for child nodes
	Key         string   // short node's key

	Prefix string // root node has no prefix (caution: same nodes can have different Prefixes, in single state trie, with little probability)

	// stats for sub trie (whose root is this node)
	// so total nodes num of this sub trie is always >= 1
	// to efficiently measure trie node num, dynamic programming
	SubTrieNodeStat NodeStat //

	Depth uint // depth of this node in state trie (root node: 0 depth)

	// ParentShortNodeNum uint   // how many parent short nodes above this node
}

// NodeStat stores 3 types of nodes' num & size
type NodeStat struct {
	// total nodes num = FullNodesNum + ShortNodesNum + LeafNodesNum
	FullNodesNum  uint64
	ShortNodesNum uint64 // short node which is not leaf node
	LeafNodesNum  uint64 // short node which is leaf node

	// total nodes size = FullNodesSize + ShortNodesSize + LeafNodesSize
	FullNodesSize  uint64
	ShortNodesSize uint64 // short node which is not leaf node
	LeafNodesSize  uint64 // short node which is leaf node

	// trie depth stats (for including trie inspect results)
	MinDepth int
	MaxDepth int
	AvgDepth float64
}

// RestoreStat logs stats for all restorations in a single block
type RestoreStat struct {
	RestorationNum       int // restore request num
	RestoredAccountNum   int // restored accounts num
	BloomFilterNum       int // (for Ethanos only) # of bloom filters for void-proof
	MerkleProofNum       int // # of merkle proofs for membership/void-proof
	MerkleProofsSize     int // total size of merkle proofs
	MerkleProofsNodesNum int // # of nodes in merkle proofs

	MinProofSize                 int // min restore proof size in this block
	MaxProofSize                 int // max restore proof size in this block
	VoidMerkleProofNumAtMaxProof int // # of void merkle proof when restore proof size was max
	FirstEpochNumAtMaxProof      int // first epoch num of the restoration when restore proof size was max
	MaxVoidMerkleProofNum        int // # of max void merkle proof num in this block
}

// BlockInfo stores block related information
type BlockInfo struct {
	Root              Hash   // root hash of trie
	InactiveRoot      Hash   // root hash of inactive trie for Ethane
	FlushedNodeHashes []Hash // hashes of flushed nodes
	MaxAccountNonce   uint64 // biggest nonce amoung accounts (just to regenerate same state root, not essential field)

	NewNodeStat           NodeStat // stats for newly flushed state trie nodes in this block
	TotalNodeStat         NodeStat // stats for total state trie data until this block
	NewStorageNodeStat    NodeStat // stats for newly flushed storage trie nodes in this block
	TotalStorageNodeStat  NodeStat // stats for total storage trie data until this block
	NewInactiveNodeStat   NodeStat // stats for newly flushed inactive trie nodes in this block
	TotalInactiveNodeStat NodeStat // stats for total inactive trie data until this block

	BlockInterval           int64 // time to generate block
	TimeToDelete            int64 // time to delete previous leaf nodes in Ethane
	TimeToInactivate        int64 // time to inactivate old leaf nodes in Ethane
	TimeToFlushActiveMem    int64 // time to flush active/state trie nodes to mem db
	TimeToFlushActiveDisk   int64 // time to flush active/state trie nodes to disk
	TimeToFlushInactiveMem  int64 // time to flush inactive trie nodes to mem db
	TimeToFlushInactiveDisk int64 // time to flush inactive trie nodes to disk
	TimeToFlushStorage      int64 // time to flush storage trie nodes

	BlockRestoreStat RestoreStat // stats for all restoration in this block

	DeletedActiveNodeNum   uint64 // total deleted leaf nodes in active trie
	DeletedInactiveNodeNum uint64 // total deleted leaf nodes in inactive trie (# of used Merkle proofs for restoration)
	InactivatedNodeNum     uint64 // total inactivated leaf nodes in active trie (= InactiveBoundaryKey)

	// total address num = active + restored + crumb + inactive address num
	ActiveAddressNum   int // # of active addressses (but not restored & not crumb)
	RestoredAddressNum int // # of restored addresses (these are also active addresses)
	CrumbAddressNum    int // # of crumb addresses these are also active addresses)
	InactiveAddressNum int // # of inactive addresses
}

// TrieGraphInfo has edges and features representing trie
type TrieGraphInfo struct {
	// Edges: [[hash1, hash2], [hash1, hash3], ...]
	Edges [][]string

	// Features[nodeHash] = feature map of this node
	// 1. ["type"] = node type
	// "0": leaf node
	// "1": short node
	// "2": full node
	// 2. ["size"] = node size
	// 3. ["childHashes"] = hashes of child nodes
	Features map[string]map[string]string
}

// Hash represents the 32 byte Keccak256 hash of arbitrary data.
type Hash [HashLength]byte

// Reset sets all NodeStat fields to 0
func (ns *NodeStat) Reset() {
	ns.FullNodesNum = 0
	ns.ShortNodesNum = 0
	ns.LeafNodesNum = 0

	ns.FullNodesSize = 0
	ns.ShortNodesSize = 0
	ns.LeafNodesSize = 0
}

// GetSum returns sum of num & size
func (ns NodeStat) GetSum() (uint64, uint64) {
	return ns.FullNodesNum + ns.ShortNodesNum + ns.LeafNodesNum,
		ns.FullNodesSize + ns.ShortNodesSize + ns.LeafNodesSize
}

// Print shows all nums & sizes
func (ns NodeStat) Print() {
	totalNum, totalSize := ns.GetSum()
	fmt.Println("  total node num:\t", totalNum)
	fmt.Println("    full node num:\t", ns.FullNodesNum)
	fmt.Println("    short node num:\t", ns.ShortNodesNum)
	fmt.Println("    leaf node num:\t", ns.LeafNodesNum)

	fmt.Println("  total node size:\t", totalSize)
	fmt.Println("    full node size:\t", ns.FullNodesSize)
	fmt.Println("    short node size:\t", ns.ShortNodesSize)
	fmt.Println("    leaf node size:\t", ns.LeafNodesSize)
}

// ToString collects values and converts them to string
func (ns NodeStat) ToString(delimiter string) string {

	totalNum, totalSize := ns.GetSum()
	values := make([]uint64, 8)
	values[0] = totalNum
	values[1] = totalSize
	values[2] = ns.FullNodesNum
	values[3] = ns.FullNodesSize
	values[4] = ns.ShortNodesNum
	values[5] = ns.ShortNodesSize
	values[6] = ns.LeafNodesNum
	values[7] = ns.LeafNodesSize

	str := ""
	for _, value := range values {
		str += strconv.FormatUint(value, 10)
		str += delimiter
	}
	return str
}

// ToString collects values and converts them to string
func (ns NodeStat) ToStringWithDepths(delimiter string) string {

	totalNum, totalSize := ns.GetSum()
	values := make([]uint64, 10)
	values[0] = totalNum
	values[1] = totalSize
	values[2] = ns.FullNodesNum
	values[3] = ns.FullNodesSize
	values[4] = ns.ShortNodesNum
	values[5] = ns.ShortNodesSize
	values[6] = ns.LeafNodesNum
	values[7] = ns.LeafNodesSize
	values[8] = uint64(ns.MinDepth)
	values[9] = uint64(ns.MaxDepth)

	str := ""
	for _, value := range values {
		str += strconv.FormatUint(value, 10)
		str += delimiter
	}

	str += fmt.Sprintf("%f", ns.AvgDepth)
	str += delimiter

	return str
}

// Reset sets all RestoreStat fields to 0
func (rs *RestoreStat) Reset() {
	rs.RestorationNum = 0
	rs.RestoredAccountNum = 0
	rs.MerkleProofNum = 0
	rs.BloomFilterNum = 0
	rs.MerkleProofsSize = 0
	rs.MerkleProofsNodesNum = 0
	rs.MinProofSize = 0
	rs.MaxProofSize = 0

	rs.VoidMerkleProofNumAtMaxProof = 0
	rs.FirstEpochNumAtMaxProof = 0
	rs.MaxVoidMerkleProofNum = 0
}

// ToString collects values and converts them to string
func (rs *RestoreStat) ToString(delimiter string) string {

	values := make([]int, 6)
	values[0] = rs.RestorationNum
	values[1] = rs.RestoredAccountNum
	values[2] = rs.BloomFilterNum
	values[3] = rs.MerkleProofNum
	values[4] = rs.MerkleProofsSize
	values[5] = rs.MerkleProofsNodesNum
	// TODO(jmlee): add min/max proof size

	str := ""
	for _, value := range values {
		str += strconv.Itoa(value)
		str += delimiter
	}
	return str
}

// Uint64ToHash converts uint64 to hex key (ex. 10 -> 0x0...0a) (jmlee)
func Uint64ToHash(i uint64) Hash {
	return HexToHash(strconv.FormatUint(i, 16))
}

// HashToUint64 converts hash to uint64 (ex. 0x0...0a -> 10) (jmlee)
// caution: hash might be bigger than max uint64 value
func HashToUint64(h Hash) uint64 {
	i, _ := strconv.ParseUint(h.Hex()[2:], 16, 64)
	return i
}

// BytesToHash sets b to hash.
// If b is larger than len(h), b will be cropped from the left.
func BytesToHash(b []byte) Hash {
	var h Hash
	h.SetBytes(b)
	return h
}

// BigToHash sets byte representation of b to hash.
// If b is larger than len(h), b will be cropped from the left.
func BigToHash(b *big.Int) Hash { return BytesToHash(b.Bytes()) }

// HexToHash sets byte representation of s to hash.
// If b is larger than len(h), b will be cropped from the left.
func HexToHash(s string) Hash { return BytesToHash(FromHex(s)) }

// Bytes gets the byte representation of the underlying hash.
func (h Hash) Bytes() []byte { return h[:] }

// Big converts a hash to a big integer.
func (h Hash) Big() *big.Int { return new(big.Int).SetBytes(h[:]) }

// Hex converts a hash to a hex string.
func (h Hash) Hex() string { return hexutil.Encode(h[:]) }

// TerminalString implements log.TerminalStringer, formatting a string for console
// output during logging.
func (h Hash) TerminalString() string {
	return fmt.Sprintf("%x..%x", h[:3], h[29:])
}

// String implements the stringer interface and is used also by the logger when
// doing full logging into a file.
func (h Hash) String() string {
	return h.Hex()
}

// Format implements fmt.Formatter.
// Hash supports the %v, %s, %q, %x, %X and %d format verbs.
func (h Hash) Format(s fmt.State, c rune) {
	hexb := make([]byte, 2+len(h)*2)
	copy(hexb, "0x")
	hex.Encode(hexb[2:], h[:])

	switch c {
	case 'x', 'X':
		if !s.Flag('#') {
			hexb = hexb[2:]
		}
		if c == 'X' {
			hexb = bytes.ToUpper(hexb)
		}
		fallthrough
	case 'v', 's':
		s.Write(hexb)
	case 'q':
		q := []byte{'"'}
		s.Write(q)
		s.Write(hexb)
		s.Write(q)
	case 'd':
		fmt.Fprint(s, ([len(h)]byte)(h))
	default:
		fmt.Fprintf(s, "%%!%c(hash=%x)", c, h)
	}
}

// UnmarshalText parses a hash in hex syntax.
func (h *Hash) UnmarshalText(input []byte) error {
	return hexutil.UnmarshalFixedText("Hash", input, h[:])
}

// UnmarshalJSON parses a hash in hex syntax.
func (h *Hash) UnmarshalJSON(input []byte) error {
	return hexutil.UnmarshalFixedJSON(hashT, input, h[:])
}

// MarshalText returns the hex representation of h.
func (h Hash) MarshalText() ([]byte, error) {
	return hexutil.Bytes(h[:]).MarshalText()
}

// SetBytes sets the hash to the value of b.
// If b is larger than len(h), b will be cropped from the left.
func (h *Hash) SetBytes(b []byte) {
	if len(b) > len(h) {
		b = b[len(b)-HashLength:]
	}

	copy(h[HashLength-len(b):], b)
}

// Generate implements testing/quick.Generator.
func (h Hash) Generate(rand *rand.Rand, size int) reflect.Value {
	m := rand.Intn(len(h))
	for i := len(h) - 1; i > m; i-- {
		h[i] = byte(rand.Uint32())
	}
	return reflect.ValueOf(h)
}

// Scan implements Scanner for database/sql.
func (h *Hash) Scan(src interface{}) error {
	srcB, ok := src.([]byte)
	if !ok {
		return fmt.Errorf("can't scan %T into Hash", src)
	}
	if len(srcB) != HashLength {
		return fmt.Errorf("can't scan []byte of len %d into Hash, want %d", len(srcB), HashLength)
	}
	copy(h[:], srcB)
	return nil
}

// Value implements valuer for database/sql.
func (h Hash) Value() (driver.Value, error) {
	return h[:], nil
}

// ImplementsGraphQLType returns true if Hash implements the specified GraphQL type.
func (Hash) ImplementsGraphQLType(name string) bool { return name == "Bytes32" }

// UnmarshalGraphQL unmarshals the provided GraphQL query data.
func (h *Hash) UnmarshalGraphQL(input interface{}) error {
	var err error
	switch input := input.(type) {
	case string:
		err = h.UnmarshalText([]byte(input))
	default:
		err = fmt.Errorf("unexpected type %T for Hash", input)
	}
	return err
}

// UnprefixedHash allows marshaling a Hash without 0x prefix.
type UnprefixedHash Hash

// UnmarshalText decodes the hash from hex. The 0x prefix is optional.
func (h *UnprefixedHash) UnmarshalText(input []byte) error {
	return hexutil.UnmarshalFixedUnprefixedText("UnprefixedHash", input, h[:])
}

// MarshalText encodes the hash as hex.
func (h UnprefixedHash) MarshalText() ([]byte, error) {
	return []byte(hex.EncodeToString(h[:])), nil
}

/////////// Address

// Address represents the 20 byte address of an Ethereum account.
type Address [AddressLength]byte

// BytesToAddress returns Address with value b.
// If b is larger than len(h), b will be cropped from the left.
func BytesToAddress(b []byte) Address {
	var a Address
	a.SetBytes(b)
	return a
}

// BigToAddress returns Address with byte values of b.
// If b is larger than len(h), b will be cropped from the left.
func BigToAddress(b *big.Int) Address { return BytesToAddress(b.Bytes()) }

// HexToAddress returns Address with byte values of s.
// If s is larger than len(h), s will be cropped from the left.
func HexToAddress(s string) Address { return BytesToAddress(FromHex(s)) }

// IsHexAddress verifies whether a string can represent a valid hex-encoded
// Ethereum address or not.
func IsHexAddress(s string) bool {
	if has0xPrefix(s) {
		s = s[2:]
	}
	return len(s) == 2*AddressLength && isHex(s)
}

// Bytes gets the string representation of the underlying address.
func (a Address) Bytes() []byte { return a[:] }

// Hash converts an address to a hash by left-padding it with zeros.
func (a Address) Hash() Hash { return BytesToHash(a[:]) }

// Hex returns an EIP55-compliant hex string representation of the address.
func (a Address) Hex() string {
	return string(a.checksumHex())
}

// String implements fmt.Stringer.
func (a Address) String() string {
	return a.Hex()
}

func (a *Address) checksumHex() []byte {
	buf := a.hex()

	// compute checksum
	sha := sha3.NewLegacyKeccak256()
	sha.Write(buf[2:])
	hash := sha.Sum(nil)
	for i := 2; i < len(buf); i++ {
		hashByte := hash[(i-2)/2]
		if i%2 == 0 {
			hashByte = hashByte >> 4
		} else {
			hashByte &= 0xf
		}
		if buf[i] > '9' && hashByte > 7 {
			buf[i] -= 32
		}
	}
	return buf[:]
}

func (a Address) hex() []byte {
	var buf [len(a)*2 + 2]byte
	copy(buf[:2], "0x")
	hex.Encode(buf[2:], a[:])
	return buf[:]
}

// Format implements fmt.Formatter.
// Address supports the %v, %s, %q, %x, %X and %d format verbs.
func (a Address) Format(s fmt.State, c rune) {
	switch c {
	case 'v', 's':
		s.Write(a.checksumHex())
	case 'q':
		q := []byte{'"'}
		s.Write(q)
		s.Write(a.checksumHex())
		s.Write(q)
	case 'x', 'X':
		// %x disables the checksum.
		hex := a.hex()
		if !s.Flag('#') {
			hex = hex[2:]
		}
		if c == 'X' {
			hex = bytes.ToUpper(hex)
		}
		s.Write(hex)
	case 'd':
		fmt.Fprint(s, ([len(a)]byte)(a))
	default:
		fmt.Fprintf(s, "%%!%c(address=%x)", c, a)
	}
}

// SetBytes sets the address to the value of b.
// If b is larger than len(a), b will be cropped from the left.
func (a *Address) SetBytes(b []byte) {
	if len(b) > len(a) {
		b = b[len(b)-AddressLength:]
	}
	copy(a[AddressLength-len(b):], b)
}

// MarshalText returns the hex representation of a.
func (a Address) MarshalText() ([]byte, error) {
	return hexutil.Bytes(a[:]).MarshalText()
}

// UnmarshalText parses a hash in hex syntax.
func (a *Address) UnmarshalText(input []byte) error {
	return hexutil.UnmarshalFixedText("Address", input, a[:])
}

// UnmarshalJSON parses a hash in hex syntax.
func (a *Address) UnmarshalJSON(input []byte) error {
	return hexutil.UnmarshalFixedJSON(addressT, input, a[:])
}

// Scan implements Scanner for database/sql.
func (a *Address) Scan(src interface{}) error {
	srcB, ok := src.([]byte)
	if !ok {
		return fmt.Errorf("can't scan %T into Address", src)
	}
	if len(srcB) != AddressLength {
		return fmt.Errorf("can't scan []byte of len %d into Address, want %d", len(srcB), AddressLength)
	}
	copy(a[:], srcB)
	return nil
}

// Value implements valuer for database/sql.
func (a Address) Value() (driver.Value, error) {
	return a[:], nil
}

// ImplementsGraphQLType returns true if Hash implements the specified GraphQL type.
func (a Address) ImplementsGraphQLType(name string) bool { return name == "Address" }

// UnmarshalGraphQL unmarshals the provided GraphQL query data.
func (a *Address) UnmarshalGraphQL(input interface{}) error {
	var err error
	switch input := input.(type) {
	case string:
		err = a.UnmarshalText([]byte(input))
	default:
		err = fmt.Errorf("unexpected type %T for Address", input)
	}
	return err
}

// UnprefixedAddress allows marshaling an Address without 0x prefix.
type UnprefixedAddress Address

// UnmarshalText decodes the address from hex. The 0x prefix is optional.
func (a *UnprefixedAddress) UnmarshalText(input []byte) error {
	return hexutil.UnmarshalFixedUnprefixedText("UnprefixedAddress", input, a[:])
}

// MarshalText encodes the address as hex.
func (a UnprefixedAddress) MarshalText() ([]byte, error) {
	return []byte(hex.EncodeToString(a[:])), nil
}

// MixedcaseAddress retains the original string, which may or may not be
// correctly checksummed
type MixedcaseAddress struct {
	addr     Address
	original string
}

// NewMixedcaseAddress constructor (mainly for testing)
func NewMixedcaseAddress(addr Address) MixedcaseAddress {
	return MixedcaseAddress{addr: addr, original: addr.Hex()}
}

// NewMixedcaseAddressFromString is mainly meant for unit-testing
func NewMixedcaseAddressFromString(hexaddr string) (*MixedcaseAddress, error) {
	if !IsHexAddress(hexaddr) {
		return nil, errors.New("invalid address")
	}
	a := FromHex(hexaddr)
	return &MixedcaseAddress{addr: BytesToAddress(a), original: hexaddr}, nil
}

// UnmarshalJSON parses MixedcaseAddress
func (ma *MixedcaseAddress) UnmarshalJSON(input []byte) error {
	if err := hexutil.UnmarshalFixedJSON(addressT, input, ma.addr[:]); err != nil {
		return err
	}
	return json.Unmarshal(input, &ma.original)
}

// MarshalJSON marshals the original value
func (ma *MixedcaseAddress) MarshalJSON() ([]byte, error) {
	if strings.HasPrefix(ma.original, "0x") || strings.HasPrefix(ma.original, "0X") {
		return json.Marshal(fmt.Sprintf("0x%s", ma.original[2:]))
	}
	return json.Marshal(fmt.Sprintf("0x%s", ma.original))
}

// Address returns the address
func (ma *MixedcaseAddress) Address() Address {
	return ma.addr
}

// String implements fmt.Stringer
func (ma *MixedcaseAddress) String() string {
	if ma.ValidChecksum() {
		return fmt.Sprintf("%s [chksum ok]", ma.original)
	}
	return fmt.Sprintf("%s [chksum INVALID]", ma.original)
}

// ValidChecksum returns true if the address has valid checksum
func (ma *MixedcaseAddress) ValidChecksum() bool {
	return ma.original == ma.addr.Hex()
}

// Original returns the mixed-case input string
func (ma *MixedcaseAddress) Original() string {
	return ma.original
}
