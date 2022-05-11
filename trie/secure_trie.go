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

package trie

import (
	"fmt"
	"os"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

// SecureTrie wraps a trie with key hashing. In a secure trie, all
// access operations hash the key using keccak256. This prevents
// calling code from creating long chains of nodes that
// increase the access time.
//
// Contrary to a regular trie, a SecureTrie can only be created with
// New and must have an attached database. The database also stores
// the preimage of each key.
//
// SecureTrie is not safe for concurrent use.
type SecureTrie struct {
	trie             Trie
	hashKeyBuf       [common.HashLength]byte
	secKeyCache      map[string][]byte
	secKeyCacheOwner *SecureTrie // Pointer to self, replace the key cache on mismatch
}

// NewSecure creates a trie with an existing root node from a backing database
// and optional intermediate in-memory node pool.
//
// If root is the zero hash or the sha3 hash of an empty string, the
// trie is initially empty. Otherwise, New will panic if db is nil
// and returns MissingNodeError if the root node cannot be found.
//
// Accessing the trie loads nodes from the database or node pool on demand.
// Loaded nodes are kept around until their 'cache generation' expires.
// A new cache generation is created by each call to Commit.
// cachelimit sets the number of past cache generations to keep.
func NewSecure(root common.Hash, db *Database) (*SecureTrie, error) {
	if db == nil {
		panic("trie.NewSecure called without a database")
	}
	trie, err := New(root, db)
	if err != nil {
		return nil, err
	}
	return &SecureTrie{trie: *trie}, nil
}

// Get returns the value for key stored in the trie.
// The value bytes must not be modified by the caller.
func (t *SecureTrie) Get(key []byte) []byte {
	res, err := t.TryGet(key)
	if err != nil {
		log.Error(fmt.Sprintf("Unhandled trie error: %v", err))
	}
	return res
}

// TryGet returns the value for key stored in the trie.
// The value bytes must not be modified by the caller.
// If a node was not found in the database, a MissingNodeError is returned.
func (t *SecureTrie) TryGet(key []byte) ([]byte, error) {
	return t.trie.TryGet(t.hashKey(key))
}

// TryGetNode attempts to retrieve a trie node by compact-encoded path. It is not
// possible to use keybyte-encoding as the path might contain odd nibbles.
func (t *SecureTrie) TryGetNode(path []byte) ([]byte, int, error) {
	return t.trie.TryGetNode(path)
}

// TryUpdate account will abstract the write of an account to the
// secure trie.
func (t *SecureTrie) TryUpdateAccount(key []byte, acc *types.StateAccount, txHash common.Hash) error {
	hk := t.hashKey(key)
	data, err := rlp.EncodeToBytes(acc)
	if err != nil {
		return err
	}

	before := t.trie.Hash()
	if err := t.trie.TryUpdate(hk, data); err != nil {
		return err
	}

	// jhkim: txdetail
	if t.trie.Hash() != before { // check whether state trie is modified or not
		addr := common.BytesToAddress(key)

		if txHash != common.HexToHash("0x0") { // 0x0 txHash should be only in genesis block or.. miner?
			writeAddrHashToAddr(hk, key) // write addresshash - address pair
			common.TxDetail[txHash].AccountBalance[common.BytesToAddress(key)] = acc.Balance.Uint64()

			// 3 kinds of account update: tx To & From, miner & uncle, internal transaction
			if addr == common.GlobalTxTo || addr == common.GlobalTxFrom { // tx To & From
				writeTxAdrHash(txHash, hk, key) // to, from address update

			} else if addr == common.GlobalBlockMiner { // miner and uncle rewarded stateTransition reward. Ignore these balance update
				// Do nothing
				// fmt.Println("miner rewarded state transition reward", common.GlobalBlockNumber, addr, txHash)
			} else if containAddress(addr, common.GlobalBlockUncles) {
				// Do nothing
				fmt.Println("uncle rewarded state transition reward", common.GlobalBlockNumber, addr, txHash)
				os.Exit(0)

			} else { //internal transaction
				common.GlobalMutex.Lock()
				TI, ok := common.TxDetail[txHash]
				common.GlobalMutex.Unlock()

				if ok {
					if common.BytesToHash(acc.CodeHash) == common.HexToHash("c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470") { // empty codehash
						// EOA
						writeTxInternal(txHash, key, true) //  EOA's state trie updates made by internal tx except from and to.
					} else {
						// CA
						if _, ok := TI.ContractAddress_SlotHash[addr]; !ok {
							writeTxInternal(txHash, key, false) //  CAs have no storage trie update, but balance update
						}
					}

				} else {
					fmt.Println("Error: Want to write trieUpdate of Contract Address, but there is no txDetail")
					fmt.Println(txHash, addr, "TI: ", TI)
					os.Exit(0)
				}
			}

		} else { // 0x0 txHash. block #0
			// fmt.Println("Unknown account update", common.GlobalBlockNumber, txHash, addr, acc.Balance.Uint64())
			if common.GlobalBlockNumber == 0 {

				if len(common.ZeroBlockTI.AccountBalance) == 0 {
					common.ZeroBlockTI.AccountBalance = map[common.Address]uint64{}
				}
				common.ZeroBlockTI.AccountBalance[addr] = acc.Balance.Uint64()
				common.TrieUpdateElseTemp = append(common.TrieUpdateElseTemp, addr) // unknown account update

			} else if addr == common.GlobalBlockMiner || containAddress(addr, common.GlobalBlockUncles) { // miner and uncles of each block
				if len(common.MinerUnlce) == 0 {
					common.MinerUnlce[common.GlobalBlockNumber] = map[common.Address]uint64{}
				}

				if len(common.MinerUnlce[common.GlobalBlockNumber]) == 0 {
					tmp := map[common.Address]uint64{}
					tmp[addr] = acc.Balance.Uint64()
					common.MinerUnlce[common.GlobalBlockNumber] = tmp
				} else {
					common.MinerUnlce[common.GlobalBlockNumber][addr] = acc.Balance.Uint64()
				}

			}

		}
	} else if t.trie.Hash() == common.HexToHash("0x0") {
		fmt.Println("0x0 root hash, because only read cached node. After commit or some cache calculation, it will be reflected")
		os.Exit(0)
	}

	t.getSecKeyCache()[string(hk)] = common.CopyBytes(key)
	return nil
}

func containAddress(key common.Address, slice []common.Address) bool {
	for _, a := range slice {
		if a == key {
			return true
		}
	}
	return false
}

// Update associates key with value in the trie. Subsequent calls to
// Get will return value. If value has length zero, any existing value
// is deleted from the trie and calls to Get will return nil.
//
// The value bytes must not be modified by the caller while they are
// stored in the trie.
func (t *SecureTrie) Update(key, value []byte) {
	if err := t.TryUpdate(key, value); err != nil {
		log.Error(fmt.Sprintf("Unhandled trie error: %v", err))
	}
}

// TryUpdate associates key with value in the trie. Subsequent calls to
// Get will return value. If value has length zero, any existing value
// is deleted from the trie and calls to Get will return nil.
//
// The value bytes must not be modified by the caller while they are
// stored in the trie.
//
// If a node was not found in the database, a MissingNodeError is returned.
func (t *SecureTrie) TryUpdate(key, value []byte) error {
	hk := t.hashKey(key)
	// if common.GlobalBlockNumber == 116525 {
	// 	fmt.Println("116525 TryUpdate/ key:", common.BytesToAddress(key), "value:", common.Bytes2Hex(value))
	// }
	err := t.trie.TryUpdate(hk, value)
	if err != nil {
		return err
	}
	t.getSecKeyCache()[string(hk)] = common.CopyBytes(key)
	return nil
}

var Mutex = new(sync.Mutex)

func writeTxAdrHash(txHash common.Hash, hk, key []byte) {
	common.GlobalMutex.Lock()
	defer common.GlobalMutex.Unlock()

	if TI, ok := common.TxDetail[txHash]; ok { // concurrent map read and map write

		if TI.From == common.BytesToAddress(key) {
			TI.FromAdrHash = common.BytesToHash(hk)
			common.TxDetail[txHash] = TI
		} else if TI.To == common.BytesToAddress(key) {
			TI.ToAdrHash = common.BytesToHash(hk)
			common.TxDetail[txHash] = TI
		}
	} else {
		fmt.Println("writeTxAddrHash NotExist?/ txhash:", txHash, "globalTxHash", common.GlobalTxHash, "blocknumber:", common.GlobalBlockNumber, "key:", common.BytesToAddress(key))
		os.Exit(0)
	}

}

func writeTxInternal(txHash common.Hash, key []byte, isEOA bool) {
	common.GlobalMutex.Lock()
	defer common.GlobalMutex.Unlock()

	if TI, ok := common.TxDetail[txHash]; ok {
		if isEOA {
			if !containAddress(common.BytesToAddress(key), TI.Internal) {
				TI.Internal = append(TI.Internal, common.BytesToAddress(key))
			}
		} else {
			if !containAddress(common.BytesToAddress(key), TI.InternalCA) {
				TI.InternalCA = append(TI.InternalCA, common.BytesToAddress(key))
			}
		}

		common.TxDetail[txHash] = TI
	} else {
		fmt.Println("writeTxInternal NotExist?/ txhash:", txHash, "globalTxHash", common.GlobalTxHash, "blocknumber:", common.GlobalBlockNumber, "key:", common.BytesToAddress(key))
		os.Exit(0)
	}
}

func writeAddrHashToAddr(hk, key []byte) {
	common.GlobalMutex.Lock()
	defer common.GlobalMutex.Unlock()

	if _, ok := common.AddrHash2Addr[common.BytesToHash(hk)]; !ok { // if not exist
		common.AddrHash2Addr[common.BytesToHash(hk)] = common.BytesToAddress(key)
	} else {
		// Do nothing

	}

}

// write updated contractAccount's SlotHash
func writeContractAccountSlotHash(txHash, slotHash common.Hash, addr common.Address, slot common.Address) {
	// if value, ok := common.TxDetailSyncMap.Load(txHash); ok { // concurrent map read and map write
	common.GlobalMutex.Lock()
	defer common.GlobalMutex.Unlock()
	// fmt.Println("write ContractAccountSlotHash", txHash, addr, slotHash)
	if TI, ok := common.TxDetail[txHash]; ok {
		// if common.GlobalBlockNumber == 72202 {
		// 	fmt.Println("TI.ContractAddress_SlotHash", common.GlobalBlockNumber, txHash, slot, slotHash)
		// }
		if tmp, ok := TI.ContractAddress_SlotHash[addr]; ok {
			if !containHash(slotHash, *tmp) {
				*tmp = append(*tmp, slotHash)
				TI.ContractAddress_SlotHash[addr] = tmp
				common.TxDetail[txHash] = TI
			}
		} else {

			TI.ContractAddress_SlotHash[addr] = &[]common.Hash{slotHash}
		}
	} else {
		fmt.Println("writeContractAccountSlotHash NotExist?/ txhash:", txHash, "globalTxHash", common.GlobalTxHash, "blocknumber:", common.GlobalBlockNumber, "addr:", addr)
		os.Exit(0)
	}

}

// jhkim
// TryUpdate function for storage trie. write updated slothash of storage trie into common.TxDetail.ContractAddress_SlotHash
func (t *SecureTrie) MyTryUpdate(key, value []byte, txHash common.Hash, addr common.Address) error {
	hk := t.hashKey(key)
	CAaddress := addr
	slotHash := common.BytesToHash(hk)
	writeSlotHashToSlot(hk, key, value, addr)

	if txHash != common.HexToHash("0x0") {
		// fmt.Println("MyTryUpdate", txHash, CAaddress, slotHash)
		writeContractAccountSlotHash(txHash, slotHash, CAaddress, common.BytesToAddress(key))

	} else { // contract account update with 0x0 txhash. never enter this branch
		fmt.Println("MyTryUpdate", common.GlobalTxHash, CAaddress, slotHash)

		writeContractAccountSlotHash(common.GlobalTxHash, slotHash, CAaddress, common.BytesToAddress(key))
		os.Exit(0)
	}

	err := t.trie.TryUpdate(hk, value)
	if err != nil {
		return err
	}

	//jhkim: check dupFlushedNode
	// if addr == common.HexToAddress("0xcbF44b4df14B264062aFa9F2486142335a983857") {
	// 	if common.GlobalBlockNumber > 432000 && common.GlobalBlockNumber < 432400 {
	// 		// if common.GlobalBlockNumber == 432286 || common.GlobalBlockNumber == 432361 || common.GlobalBlockNumber == 432368 {
	// 		fmt.Println("block", common.GlobalBlockNumber, "MyTryUpdate/ key", common.BytesToAddress(key), "value", common.Bytes2Hex(value), "    hashkey", common.BytesToHash(hk))
	// 	}
	// 	// t.trie.Print()
	// }

	t.getSecKeyCache()[string(hk)] = common.CopyBytes(key)
	return nil
}

func writeSlotHashToSlot(hk, key, value []byte, addr common.Address) {
	common.GlobalMutex.Lock()
	defer common.GlobalMutex.Unlock()
	// fmt.Println("    SH2S/block#", common.GlobalBlockNumber, "addr:", addr, "slot:", common.BytesToAddress(key), "slotHash", common.BytesToHash(hk), "value:", common.Bytes2Hex(value))
	// if common.IsPrefetch {
	// 	fmt.Println("    writeSlotHashToSlot/ slot:", common.BytesToHash(key), ", slotHash", common.BytesToAddress(hk))
	// }

	// if _, ok := common.AddrHash2Addr[common.BytesToHash(hk)]; !ok {
	// 	common.AddrHash2Addr[common.BytesToHash(hk)] = common.BytesToAddress(key)
	// }
	tmp := common.SlotDetail{
		Hashkey:     common.BytesToHash(hk),
		Key:         common.BytesToAddress(key),
		Value:       value,
		Blocknumber: common.GlobalBlockNumber,
	}

	// = [4][]byte{hk, key, value, []byte(strconv.Itoa(common.GlobalBlockNumber))}
	common.Account_SlotHash2Slot[addr] = append(common.Account_SlotHash2Slot[addr], tmp)
	// 새로 변수를 만들어서 넣어보자
	if common.GlobalBlockNumber == 97321 {
		for _, v := range common.Account_SlotHash2Slot[addr] {
			if v.Blocknumber == 97321 {

				// fmt.Println("      hashkey:", v.Hashkey, "key:", v.Key, "blocknumber:", v.Blocknumber, "value:", common.Bytes2Hex(v.Value))
			}

		}

	}
	// }

}

// Delete removes any existing value for key from the trie.
func (t *SecureTrie) Delete(key []byte) {
	if err := t.TryDelete(key); err != nil {
		log.Error(fmt.Sprintf("Unhandled trie error: %v", err))
	}
}

// TryDelete removes any existing value for key from the trie.
// If a node was not found in the database, a MissingNodeError is returned.
func (t *SecureTrie) TryDelete(key []byte) error {
	hk := t.hashKey(key)
	delete(t.getSecKeyCache(), string(hk))
	return t.trie.TryDelete(hk)
}

// GetKey returns the sha3 preimage of a hashed key that was
// previously used to store a value.
func (t *SecureTrie) GetKey(shaKey []byte) []byte {
	if key, ok := t.getSecKeyCache()[string(shaKey)]; ok {
		return key
	}
	return t.trie.db.preimage(common.BytesToHash(shaKey))
}

// Commit writes all nodes and the secure hash pre-images to the trie's database.
// Nodes are stored with their sha3 hash as the key.
//
// Committing flushes nodes from memory. Subsequent Get calls will load nodes
// from the database.
func (t *SecureTrie) Commit(onleaf LeafCallback) (common.Hash, int, error) {
	// Write all the pre-images to the actual disk database
	if len(t.getSecKeyCache()) > 0 {
		if t.trie.db.preimages != nil { // Ugly direct check but avoids the below write lock
			t.trie.db.lock.Lock()
			for hk, key := range t.secKeyCache {
				t.trie.db.insertPreimage(common.BytesToHash([]byte(hk)), key)
			}
			t.trie.db.lock.Unlock()
		}
		t.secKeyCache = make(map[string][]byte)
	}
	// Commit the trie to its intermediate node database
	return t.trie.Commit(onleaf)
}

// Hash returns the root hash of SecureTrie. It does not write to the
// database and can be used even if the trie doesn't have one.
func (t *SecureTrie) Hash() common.Hash {
	return t.trie.Hash()
}

// Copy returns a copy of SecureTrie.
func (t *SecureTrie) Copy() *SecureTrie {
	cpy := *t
	return &cpy
}

// NodeIterator returns an iterator that returns nodes of the underlying trie. Iteration
// starts at the key after the given start key.
func (t *SecureTrie) NodeIterator(start []byte) NodeIterator {
	return t.trie.NodeIterator(start)
}

// hashKey returns the hash of key as an ephemeral buffer.
// The caller must not hold onto the return value because it will become
// invalid on the next call to hashKey or secKey.
func (t *SecureTrie) hashKey(key []byte) []byte {
	h := newHasher(false)
	h.sha.Reset()
	h.sha.Write(key)
	h.sha.Read(t.hashKeyBuf[:])
	returnHasherToPool(h)
	return t.hashKeyBuf[:]
}

// getSecKeyCache returns the current secure key cache, creating a new one if
// ownership changed (i.e. the current secure trie is a copy of another owning
// the actual cache).
func (t *SecureTrie) getSecKeyCache() map[string][]byte {
	if t != t.secKeyCacheOwner {
		t.secKeyCacheOwner = t
		t.secKeyCache = make(map[string][]byte)
	}
	return t.secKeyCache
}

// get trie of secure trie (jmlee)
func (t *SecureTrie) Trie() *Trie {
	return &t.trie
}

// trie size inspectaion (jhkim)
func (t *SecureTrie) InspectTrie() TrieInspectResult {
	return t.trie.InspectTrie()
}

func (t *SecureTrie) InspectStorageTrie() TrieInspectResult {
	return t.trie.InspectStorageTrie()
}

// jhkim
func (t *SecureTrie) Print() error {

	if t.trie.root != nil {
		fmt.Println(t.trie.root.toString("", t.trie.db))
	}
	return nil
}
