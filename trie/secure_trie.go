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
func (t *SecureTrie) TryUpdateAccount(key []byte, acc *types.StateAccount) error {
	hk := t.hashKey(key)
	data, err := rlp.EncodeToBytes(acc)
	if err != nil {
		return err
	}
	if err := t.trie.TryUpdate(hk, data); err != nil {
		return err
	}
	addr := common.BytesToAddress(key)

	fmt.Println("tryUpdateAccount", common.GlobalBlockNumber, common.GlobalTxHash, addr, acc.Balance, common.BytesToHash(acc.CodeHash), acc.Nonce, acc.Root)
	fmt.Println("  -> state trie root", t.trie.Hash())
	fmt.Println()

	if common.GlobalBlockNumber > 0 && common.GlobalTxHash == common.HexToHash("0x0") {
		if addr == common.GlobalBlockMiner {
			if sa, exist := common.BlockMinerList[common.GlobalBlockNumber][addr]; exist {
				sa.Balance = acc.Balance
				sa.Nonce = acc.Nonce
				common.BlockMinerList[common.GlobalBlockNumber][addr] = sa

			} else {
				tmp := common.SubstateAccount{

					Balance:     acc.Balance,
					Nonce:       acc.Nonce,
					CodeHash:    common.BytesToHash(acc.CodeHash),
					StorageRoot: acc.Root,
				}
				common.BlockMinerList[common.GlobalBlockNumber][addr] = tmp
			}

		} else if common.ContainsAddress(addr, common.GlobalBlockUncles) {
			if sa, exist := common.BlockUncleList[common.GlobalBlockNumber][addr]; exist {
				sa.Balance = acc.Balance
				sa.Nonce = acc.Nonce
				common.BlockUncleList[common.GlobalBlockNumber][addr] = sa

			} else {
				tmp := common.SubstateAccount{

					Balance:     acc.Balance,
					Nonce:       acc.Nonce,
					CodeHash:    common.BytesToHash(acc.CodeHash),
					StorageRoot: acc.Root,
				}
				common.BlockUncleList[common.GlobalBlockNumber][addr] = tmp
			}
		} else {
			// if common.GlobalBlockNumber != 1920000 {
			// 	panic(0)
			// }
		}
	}
	// jhkim: double check code
	// addr := common.BytesToAddress(key)
	// if addr == common.HexToAddress("0x3dbdc81a6edc94c720b0b88fb65dbd7e395fdcf6") {
	// fmt.Println("tryUpdateAccount", common.GlobalBlockNumber, common.GlobalTxHash, addr, acc.Balance, common.BytesToHash(acc.CodeHash), acc.Nonce, acc.Root)
	// fmt.Println("  -> state trie root", t.trie.Hash())
	// fmt.Println()
	// }

	if common.GlobalBlockNumber >= 4370000 && common.GlobalTxHash != common.HexToHash("0x0") {

		if sa, exist := common.TxWriteList[common.GlobalTxHash][addr]; exist {
			// fmt.Println("DOUBLECHECK", addr, acc.Balance, common.BytesToHash(acc.CodeHash), acc.Nonce, acc.Root)
			// if addr == common.HexToAddress("0x331d077518216c07c87f4f18ba64cd384c411f84") {
			// 	fmt.Println("@#@#")
			// 	fmt.Println(acc.Root)
			// 	fmt.Println(sa.StorageRoot)
			// }

			if sa.Balance != acc.Balance {
				fmt.Println("different balance", "blk#", common.GlobalBlockNumber, "txhash", common.GlobalTxHash, "address", addr)
				fmt.Println("  Writelist:", sa.Balance, "vs", "trie update:", acc.Balance)
				panic(0)
			}
			if sa.CodeHash != common.BytesToHash(acc.CodeHash) {
				fmt.Println("different Codehash", "blk#", common.GlobalBlockNumber, "txhash", common.GlobalTxHash, "address", addr)
				fmt.Println("  Writelist:", sa.CodeHash, "vs", "trie update:", common.BytesToHash(acc.CodeHash))
				panic(0)
			}
			if sa.Nonce != acc.Nonce {
				fmt.Println("different Nonce", "blk#", common.GlobalBlockNumber, "txhash", common.GlobalTxHash, "address", addr)
				fmt.Println("  Writelist:", sa.Nonce, "vs", "trie update:", acc.Nonce)
				panic(0)
			}
			if sa.StorageRoot != acc.Root {
				fmt.Println("different StorageRoot", "blk#", common.GlobalBlockNumber, "txhash", common.GlobalTxHash, "address", addr)
				fmt.Println("  Writelist:", sa.StorageRoot, "vs", "trie update:", acc.Root)
				panic(0)
			}
			if sa.CodeHash == common.EmptyCodeHash && len(sa.Code) != 0 {
				fmt.Println("Something wrong with code and codehash - emptycodehash but no 0 length code")
				fmt.Println(common.GlobalTxHash, addr)
				panic(0)
			}
			if sa.CodeHash != common.EmptyCodeHash && len(sa.Code) == 0 {
				fmt.Println("Something wrong with code and codehash - 0 length code but emptycodehash")
				fmt.Println(common.GlobalTxHash, addr)
				panic(0)
			}

		} else {
			fmt.Println("Writelist doesn't have sa", "blk#", common.GlobalBlockNumber, "txhash", common.GlobalTxHash, "address", addr)
			fmt.Println("  During write account", addr)
			panic(0)
		}
	}
	t.getSecKeyCache()[string(hk)] = common.CopyBytes(key)
	return nil
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
	// stored, err1 := t.trie.TryGet(hk)
	// if err1 != nil {
	// 	fmt.Println("TryGet err", err1)
	// }
	// fmt.Println("    TryGet: key:", common.Bytes2Hex(key), "/ hk:", common.Bytes2Hex(hk))
	// fmt.Println("        /stored:", common.Bytes2Hex(stored), "/update:", common.Bytes2Hex(value))
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
