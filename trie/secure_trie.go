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
	if t.trie.Hash() != before {
		addr := common.BytesToAddress(key)

		if txHash != common.HexToHash("0x0") { // 0x0 txHash should be only in genesis block or.. miner block?
			writeAddrHashToAddr(hk, key)

			// 3 kinds of account update: tx To & From, miner & uncle, inner transaction
			if addr == common.GlobalTxTo || addr == common.GlobalTxFrom { // tx To & From
				writeTxHash(txHash, hk, key) // to, from address update
			} else if addr == common.GlobalBlockMiner || containAddress(addr, common.GlobalBlockUncles) { // miner & uncles
				// Do nothing

			} else { //inner transaction
				common.GlobalMutex.Lock()
				TI, ok := common.TxDetail[txHash]
				common.GlobalMutex.Unlock()

				if ok { // check txDetail exists
					_, ok := TI.ContractAddress_SlotHash[addr] // why? address가 contract address에 존재한다? -> contract address의 update가 말이되나?
					if !ok {
						if common.GlobalContractAddress != addr {
							// if common.GlobalBlockNumber == 54347 {
							// 	fmt.Println("TryUpdateAccount ", "txhash", txHash, "TI.DeployedContractAddress", TI.DeployedContractAddress, "address", addr)
							// }
							writeTxElse(txHash, key) // except to and from. This includes state trie updates made by internal tx
						}
					} else { // update contract account's leaf node
						// fmt.Println("ContractAddress_Slothash contains addr", addr, "txHash", txHash)
					}
				} else {
					fmt.Println("Error: Want to write SlotHashes of ContractAddress, but there is no txDetail")
					fmt.Println(txHash, addr, "TI: ", TI)
					os.Exit(0)
				}
			}

		} else { // 0x0 txHash. miner & uncle
			if addr != common.GlobalBlockMiner && !containAddress(addr, common.GlobalBlockUncles) {
				// fmt.Println("Unknown account update", common.GlobalBlockNumber, addr, common.GlobalBlockMiner)
				common.TrieUpdateElseTemp = append(common.TrieUpdateElseTemp, addr) // unknown account update
			}

		}
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

func writeTxHash(txHash common.Hash, hk, key []byte) {
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
	}

}

func writeTxElse(txHash common.Hash, key []byte) {
	common.GlobalMutex.Lock()
	defer common.GlobalMutex.Unlock()

	// txHash = common.GlobalTxHash // for double check

	// if common.GlobalBlockNumber == 55284 {
	// 	fmt.Println("    writeTxElse", common.GlobalBlockNumber, txHash, common.BytesToAddress(key))
	// }
	if TI, ok := common.TxDetail[txHash]; ok {

		if !containAddress(common.BytesToAddress(key), TI.Else) {
			TI.Else = append(TI.Else, common.BytesToAddress(key))
		}

		common.TxDetail[txHash] = TI
	} else {
		fmt.Println("writeTxElse NotExist?/ txhash:", txHash, "globalTxHash", common.GlobalTxHash, "blocknumber:", common.GlobalBlockNumber, "key:", common.BytesToAddress(key))

	}
}

func writeAddrHashToAddr(hk, key []byte) {
	common.GlobalMutex.Lock()
	defer common.GlobalMutex.Unlock()

	if _, ok := common.AddrHash2Addr[common.BytesToHash(hk)]; !ok {
		common.AddrHash2Addr[common.BytesToHash(hk)] = common.BytesToAddress(key)
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

		// fmt.Println("Never print this line", txHash, slotHash, common.GlobalBlockNumber)
		// os.Exit(1)
	}

}

// jhkim
// TryUpdate function for storage trie. write updated slothash of storage trie into common.TxDetail.ContractAddress_SlotHash
func (t *SecureTrie) MyTryUpdate(key, value []byte, txHash common.Hash, addr common.Address) error {
	hk := t.hashKey(key)
	CAaddress := addr
	slotHash := common.BytesToHash(hk)

	if txHash != common.HexToHash("0x0") {
		// fmt.Println("MyTryUpdate", txHash, CAaddress, slotHash)
		writeContractAccountSlotHash(txHash, slotHash, CAaddress, common.BytesToAddress(key))
	} else {
		// fmt.Println("MyTryUpdate", common.GlobalTxHash, CAaddress, slotHash)
		writeContractAccountSlotHash(common.GlobalTxHash, slotHash, CAaddress, common.BytesToAddress(key))
	}

	err := t.trie.TryUpdate(hk, value)
	if err != nil {
		return err
	}
	t.getSecKeyCache()[string(hk)] = common.CopyBytes(key)
	return nil
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
