// Copyright 2014 The go-ethereum Authors
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

// Package trie implements Merkle Patricia Tries.
package trie

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"os"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

var (
	// emptyRoot is the known root hash of an empty trie.
	emptyRoot = common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")

	// emptyState is the known hash of an empty state trie entry.
	emptyState = crypto.Keccak256Hash(nil)

	// to save the leaf info while traversing the trie (joonha)
	accountsInTrie = make([][]byte, 0)
	keysToLeafNode = make([]common.Hash, 0)
)

// LeafCallback is a callback type invoked when a trie operation reaches a leaf
// node.
//
// The paths is a path tuple identifying a particular trie node either in a single
// trie (account) or a layered trie (account -> storage). Each path in the tuple
// is in the raw format(32 bytes).
//
// The hexpath is a composite hexary path identifying the trie node. All the key
// bytes are converted to the hexary nibbles and composited with the parent path
// if the trie node is in a layered trie.
//
// It's used by state sync and commit to allow handling external references
// between account and storage tries. And also it's used in the state healing
// for extracting the raw states(leaf nodes) with corresponding paths.
type LeafCallback func(paths [][]byte, hexpath []byte, leaf []byte, parent common.Hash) error

// Trie is a Merkle Patricia Trie.
// The zero value is an empty trie with no database.
// Use New to create a trie that sits on top of a database.
//
// Trie is not safe for concurrent use.
type Trie struct {
	db   *Database
	root node
	// Keep track of the number leafs which have been inserted since the last
	// hashing operation. This number will not directly map to the number of
	// actually unhashed nodes
	unhashed int
}

// newFlag returns the cache flag value for a newly created node.
func (t *Trie) newFlag() nodeFlag {
	return nodeFlag{dirty: true}
}

// New creates a trie with an existing root node from db.
//
// If root is the zero hash or the sha3 hash of an empty string, the
// trie is initially empty and does not require a database. Otherwise,
// New will panic if db is nil and returns a MissingNodeError if root does
// not exist in the database. Accessing the trie loads nodes from db on demand.
func New(root common.Hash, db *Database) (*Trie, error) {
	if db == nil {
		panic("trie.New called without a database")
	}
	trie := &Trie{
		db: db,
	}
	if root != (common.Hash{}) && root != emptyRoot {
		rootnode, err := trie.resolveHash(root[:], nil)
		if err != nil {
			return nil, err
		}
		trie.root = rootnode
	}
	return trie, nil
}

// NodeIterator returns an iterator that returns nodes of the trie. Iteration starts at
// the key after the given start key.
func (t *Trie) NodeIterator(start []byte) NodeIterator {
	return newNodeIterator(t, start)
}

// Get returns the value for key stored in the trie.
// The value bytes must not be modified by the caller.
func (t *Trie) Get(key []byte) []byte {
	res, err := t.TryGet(key)
	if err != nil {
		log.Error(fmt.Sprintf("Unhandled trie error: %v", err))
	}
	return res
}

// TryGet returns the value for key stored in the trie.
// The value bytes must not be modified by the caller.
// If a node was not found in the database, a MissingNodeError is returned.
func (t *Trie) TryGet(key []byte) ([]byte, error) {
	value, newroot, didResolve, err := t.tryGet(t.root, keybytesToHex(key), 0)
	if err == nil && didResolve {
		t.root = newroot
	}
	return value, err
}

func (t *Trie) tryGet(origNode node, key []byte, pos int) (value []byte, newnode node, didResolve bool, err error) {
	switch n := (origNode).(type) {
	case nil:
		return nil, nil, false, nil
	case valueNode:
		return n, n, false, nil
	case *shortNode:
		if len(key)-pos < len(n.Key) || !bytes.Equal(n.Key, key[pos:pos+len(n.Key)]) {
			// key not found in trie
			return nil, n, false, nil
		}
		value, newnode, didResolve, err = t.tryGet(n.Val, key, pos+len(n.Key))
		if err == nil && didResolve {
			n = n.copy()
			n.Val = newnode
		}
		return value, n, didResolve, err
	case *fullNode:
		value, newnode, didResolve, err = t.tryGet(n.Children[key[pos]], key, pos+1)
		if err == nil && didResolve {
			n = n.copy()
			n.Children[key[pos]] = newnode
		}
		return value, n, didResolve, err
	case hashNode:
		// for Ethane's light inactive trie delete (jmlee)
		// zeroHashNode means there is no leaf node
		if IsZeroHashNode(n) {
			return nil, nil, false, nil
		}

		child, err := t.resolveHash(n, key[:pos])
		if err != nil {
			return nil, n, true, err
		}
		value, newnode, _, err := t.tryGet(child, key, pos)
		return value, newnode, true, err
	default:
		panic(fmt.Sprintf("%T: invalid node: %v", origNode, origNode))
	}
}

// TryGetNode attempts to retrieve a trie node by compact-encoded path. It is not
// possible to use keybyte-encoding as the path might contain odd nibbles.
func (t *Trie) TryGetNode(path []byte) ([]byte, int, error) {
	item, newroot, resolved, err := t.tryGetNode(t.root, compactToHex(path), 0)
	if err != nil {
		return nil, resolved, err
	}
	if resolved > 0 {
		t.root = newroot
	}
	if item == nil {
		return nil, resolved, nil
	}
	return item, resolved, err
}

func (t *Trie) tryGetNode(origNode node, path []byte, pos int) (item []byte, newnode node, resolved int, err error) {
	// If non-existent path requested, abort
	if origNode == nil {
		return nil, nil, 0, nil
	}
	// If we reached the requested path, return the current node
	if pos >= len(path) {
		// Although we most probably have the original node expanded, encoding
		// that into consensus form can be nasty (needs to cascade down) and
		// time consuming. Instead, just pull the hash up from disk directly.
		var hash hashNode
		if node, ok := origNode.(hashNode); ok {
			hash = node
		} else {
			hash, _ = origNode.cache()
		}
		if hash == nil {
			return nil, origNode, 0, errors.New("non-consensus node")
		}
		blob, err := t.db.Node(common.BytesToHash(hash))
		return blob, origNode, 1, err
	}
	// Path still needs to be traversed, descend into children
	switch n := (origNode).(type) {
	case valueNode:
		// Path prematurely ended, abort
		return nil, nil, 0, nil

	case *shortNode:
		if len(path)-pos < len(n.Key) || !bytes.Equal(n.Key, path[pos:pos+len(n.Key)]) {
			// Path branches off from short node
			return nil, n, 0, nil
		}
		item, newnode, resolved, err = t.tryGetNode(n.Val, path, pos+len(n.Key))
		if err == nil && resolved > 0 {
			n = n.copy()
			n.Val = newnode
		}
		return item, n, resolved, err

	case *fullNode:
		item, newnode, resolved, err = t.tryGetNode(n.Children[path[pos]], path, pos+1)
		if err == nil && resolved > 0 {
			n = n.copy()
			n.Children[path[pos]] = newnode
		}
		return item, n, resolved, err

	case hashNode:
		child, err := t.resolveHash(n, path[:pos])
		if err != nil {
			return nil, n, 1, err
		}
		item, newnode, resolved, err := t.tryGetNode(child, path, pos)
		return item, newnode, resolved + 1, err

	default:
		panic(fmt.Sprintf("%T: invalid node: %v", origNode, origNode))
	}
}

// Update associates key with value in the trie. Subsequent calls to
// Get will return value. If value has length zero, any existing value
// is deleted from the trie and calls to Get will return nil.
//
// The value bytes must not be modified by the caller while they are
// stored in the trie.
func (t *Trie) Update(key, value []byte) {
	if err := t.TryUpdate(key, value); err != nil {
		log.Error(fmt.Sprintf("Unhandled trie error: %v", err))
	}
}

func (t *Trie) TryUpdateAccount(key []byte, acc *types.StateAccount) error {
	data, err := rlp.EncodeToBytes(acc)
	if err != nil {
		return fmt.Errorf("can't encode object at %x: %w", key[:], err)
	}
	return t.TryUpdate(key, data)
}

// TryUpdate associates key with value in the trie. Subsequent calls to
// Get will return value. If value has length zero, any existing value
// is deleted from the trie and calls to Get will return nil.
//
// The value bytes must not be modified by the caller while they are
// stored in the trie.
//
// If a node was not found in the database, a MissingNodeError is returned.
func (t *Trie) TryUpdate(key, value []byte) error {
	t.unhashed++
	k := keybytesToHex(key)
	if len(value) != 0 {
		_, n, err := t.insert(t.root, nil, k, valueNode(value))
		if err != nil {
			return err
		}
		t.root = n
	} else {
		_, n, err := t.delete(t.root, nil, k)
		if err != nil {
			return err
		}
		t.root = n
	}
	return nil
}

func (t *Trie) insert(n node, prefix, key []byte, value node) (bool, node, error) {
	if len(key) == 0 {
		if v, ok := n.(valueNode); ok {
			return !bytes.Equal(v, value.(valueNode)), value, nil
		}
		return true, value, nil
	}
	switch n := n.(type) {
	case *shortNode:
		matchlen := prefixLen(key, n.Key)
		// If the whole key matches, keep this short node as is
		// and only update the value.
		if matchlen == len(n.Key) {
			dirty, nn, err := t.insert(n.Val, append(prefix, key[:matchlen]...), key[matchlen:], value)
			if !dirty || err != nil {
				return false, n, err
			}
			return true, &shortNode{n.Key, nn, t.newFlag()}, nil
		}
		// Otherwise branch out at the index where they differ.
		branch := &fullNode{flags: t.newFlag()}
		var err error
		_, branch.Children[n.Key[matchlen]], err = t.insert(nil, append(prefix, n.Key[:matchlen+1]...), n.Key[matchlen+1:], n.Val)
		if err != nil {
			return false, nil, err
		}
		_, branch.Children[key[matchlen]], err = t.insert(nil, append(prefix, key[:matchlen+1]...), key[matchlen+1:], value)
		if err != nil {
			return false, nil, err
		}
		// Replace this shortNode with the branch if it occurs at index 0.
		if matchlen == 0 {
			return true, branch, nil
		}
		// Otherwise, replace it with a short node leading up to the branch.
		return true, &shortNode{key[:matchlen], branch, t.newFlag()}, nil

	case *fullNode:
		dirty, nn, err := t.insert(n.Children[key[0]], append(prefix, key[0]), key[1:], value)
		if !dirty || err != nil {
			return false, n, err
		}
		n = n.copy()
		n.flags = t.newFlag()
		n.Children[key[0]] = nn
		return true, n, nil

	case nil:
		return true, &shortNode{key, value, t.newFlag()}, nil

	case hashNode:

		// if this is zero hash node, this should be treated as nil
		// but there is no case to insert data at zeroHashNode in Ethane (jmlee)
		// if needed, comment out these codes
		// if IsZeroHashNode(n) {
		// 	fmt.Println("at insert(), come to zero hash node -> prefix:", prefix)
		// 	fmt.Println("  key:", key)
		// 	return true, &shortNode{key, value, t.newFlag()}, nil
		// }

		// We've hit a part of the trie that isn't loaded yet. Load
		// the node and insert into it. This leaves all child nodes on
		// the path to the value in the trie.
		rn, err := t.resolveHash(n, prefix)
		if err != nil {
			return false, nil, err
		}
		dirty, nn, err := t.insert(rn, prefix, key, value)
		if !dirty || err != nil {
			return false, rn, err
		}
		return true, nn, nil

	default:
		panic(fmt.Sprintf("%T: invalid node: %v", n, n))
	}
}

// Delete removes any existing value for key from the trie.
func (t *Trie) Delete(key []byte) {
	if err := t.TryDelete(key); err != nil {
		log.Error(fmt.Sprintf("Unhandled trie error: %v", err))
	}
}

// TryDelete removes any existing value for key from the trie.
// If a node was not found in the database, a MissingNodeError is returned.
func (t *Trie) TryDelete(key []byte) error {
	t.unhashed++
	k := keybytesToHex(key)
	_, n, err := t.delete(t.root, nil, k)
	if err != nil {
		return err
	}
	t.root = n
	return nil
}

// delete returns the new root of the trie with key deleted.
// It reduces the trie to minimal form by simplifying
// nodes on the way up after deleting recursively.
func (t *Trie) delete(n node, prefix, key []byte) (bool, node, error) {
	switch n := n.(type) {
	case *shortNode:
		matchlen := prefixLen(key, n.Key)
		if matchlen < len(n.Key) {
			return false, n, nil // don't replace n on mismatch
		}
		if matchlen == len(key) {
			return true, nil, nil // remove n entirely for whole matches
		}
		// The key is longer than n.Key. Remove the remaining suffix
		// from the subtrie. Child can never be nil here since the
		// subtrie must contain at least two other values with keys
		// longer than n.Key.
		dirty, child, err := t.delete(n.Val, append(prefix, key[:len(n.Key)]...), key[len(n.Key):])
		if !dirty || err != nil {
			return false, n, err
		}
		switch child := child.(type) {
		case *shortNode:
			// Deleting from the subtrie reduced it to another
			// short node. Merge the nodes to avoid creating a
			// shortNode{..., shortNode{...}}. Use concat (which
			// always creates a new slice) instead of append to
			// avoid modifying n.Key since it might be shared with
			// other nodes.
			return true, &shortNode{concat(n.Key, child.Key...), child.Val, t.newFlag()}, nil
		default:
			return true, &shortNode{n.Key, child, t.newFlag()}, nil
		}

	case *fullNode:
		dirty, nn, err := t.delete(n.Children[key[0]], append(prefix, key[0]), key[1:])
		if !dirty || err != nil {
			return false, n, err
		}
		n = n.copy()
		n.flags = t.newFlag()
		n.Children[key[0]] = nn

		// Because n is a full node, it must've contained at least two children
		// before the delete operation. If the new child value is non-nil, n still
		// has at least two children after the deletion, and cannot be reduced to
		// a short node.
		if nn != nil {
			return true, n, nil
		}
		// Reduction:
		// Check how many non-nil entries are left after deleting and
		// reduce the full node to a short node if only one entry is
		// left. Since n must've contained at least two children
		// before deletion (otherwise it would not be a full node) n
		// can never be reduced to nil.
		//
		// When the loop is done, pos contains the index of the single
		// value that is left in n or -2 if n contains at least two
		// values.
		pos := -1
		for i, cld := range &n.Children {
			if cld != nil {
				if pos == -1 {
					pos = i
				} else {
					pos = -2
					break
				}
			}
		}
		if pos >= 0 {
			if pos != 16 {
				// If the remaining entry is a short node, it replaces
				// n and its key gets the missing nibble tacked to the
				// front. This avoids creating an invalid
				// shortNode{..., shortNode{...}}.  Since the entry
				// might not be loaded yet, resolve it just for this
				// check.

				// for Ethane's light inactive trie delete (jmlee)
				if common.DeletingInactiveTrieFlag {
					// if single child node is zeroHashNode, delete full node n as a whole
					hn, ok := n.Children[pos].(hashNode)
					if ok && IsZeroHashNode(hn) {
						// fmt.Println("in trie.delete(): single child is zeroHashNode, delete the full node")
						common.DeletedZeroHashNodeNum++
						return true, nil, nil
					}
					// else, mark deleted child node as a zeroHashNode
					// fmt.Println("in trie.delete(): leave zeroHashNode")
					n = n.copy()
					n.flags = t.newFlag()
					n.Children[key[0]] = ZeroHashNode
					common.ZeroHashNodeNum++
					return true, n, nil
				}

				// original code
				cnode, err := t.resolve(n.Children[pos], prefix)
				if err != nil {
					return false, nil, err
				}
				if cnode, ok := cnode.(*shortNode); ok {
					k := append([]byte{byte(pos)}, cnode.Key...)
					return true, &shortNode{k, cnode.Val, t.newFlag()}, nil
				}
			}
			// Otherwise, n is replaced by a one-nibble short node
			// containing the child.
			return true, &shortNode{[]byte{byte(pos)}, n.Children[pos], t.newFlag()}, nil
		}
		// n still contains at least two values and cannot be reduced.
		return true, n, nil

	case valueNode:
		return true, nil, nil

	case nil:
		return false, nil, nil

	case hashNode:
		// We've hit a part of the trie that isn't loaded yet. Load
		// the node and delete from it. This leaves all child nodes on
		// the path to the value in the trie.
		rn, err := t.resolveHash(n, prefix)
		if err != nil {
			return false, nil, err
		}
		dirty, nn, err := t.delete(rn, prefix, key)
		if !dirty || err != nil {
			return false, rn, err
		}
		return true, nn, nil

	default:
		panic(fmt.Sprintf("%T: invalid node: %v (%v)", n, n, key))
	}
}

func concat(s1 []byte, s2 ...byte) []byte {
	r := make([]byte, len(s1)+len(s2))
	copy(r, s1)
	copy(r[len(s1):], s2)
	return r
}

func (t *Trie) resolve(n node, prefix []byte) (node, error) {
	if n, ok := n.(hashNode); ok {
		return t.resolveHash(n, prefix)
	}
	return n, nil
}

func (t *Trie) resolveHash(n hashNode, prefix []byte) (node, error) {
	hash := common.BytesToHash(n)
	if node := t.db.node(hash); node != nil {
		return node, nil
	}
	return nil, &MissingNodeError{NodeHash: hash, Path: prefix}
}

// Hash returns the root hash of the trie. It does not write to the
// database and can be used even if the trie doesn't have one.
func (t *Trie) Hash() common.Hash {
	hash, cached, _ := t.hashRoot()
	t.root = cached
	return common.BytesToHash(hash.(hashNode))
}

// Commit writes all nodes to the trie's memory database, tracking the internal
// and external (for account tries) references.
func (t *Trie) Commit(onleaf LeafCallback) (common.Hash, int, error) {
	if t.db == nil {
		panic("commit called on trie with nil database")
	}
	if t.root == nil {
		return emptyRoot, 0, nil
	}
	// Derive the hash for all dirty nodes first. We hold the assumption
	// in the following procedure that all nodes are hashed.
	rootHash := t.Hash()
	h := newCommitter()
	defer returnCommitterToPool(h)

	// Do a quick check if we really need to commit, before we spin
	// up goroutines. This can happen e.g. if we load a trie for reading storage
	// values, but don't write to it.
	if _, dirty := t.root.cache(); !dirty {
		return rootHash, 0, nil
	}
	var wg sync.WaitGroup
	if onleaf != nil {
		h.onleaf = onleaf
		h.leafCh = make(chan *leaf, leafChanSize)
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.commitLoop(t.db)
		}()
	}
	newRoot, committed, err := h.Commit(t.root, t.db)
	if onleaf != nil {
		// The leafch is created in newCommitter if there was an onleaf callback
		// provided. The commitLoop only _reads_ from it, and the commit
		// operation was the sole writer. Therefore, it's safe to close this
		// channel here.
		close(h.leafCh)
		wg.Wait()
	}
	if err != nil {
		return common.Hash{}, 0, err
	}
	t.root = newRoot
	return rootHash, committed, nil
}

// hashRoot calculates the root hash of the given trie
func (t *Trie) hashRoot() (node, node, error) {
	if t.root == nil {
		return hashNode(emptyRoot.Bytes()), nil, nil
	}
	// If the number of changes is below 100, we let one thread handle it
	h := newHasher(t.unhashed >= 100)
	defer returnHasherToPool(h)
	hashed, cached := h.hash(t.root, true)
	t.unhashed = 0
	return hashed, cached, nil
}

// Reset drops the referenced root node and cleans all internal state.
func (t *Trie) Reset() {
	t.root = nil
	t.unhashed = 0
}

// TrieDB returns its database (jmlee)
func (t *Trie) TrieDB() *Database {
	return t.db
}

// Print shows trie nodes details in human readable form (jmlee)
func (t *Trie) Print() {
	if t.Hash() == emptyRoot {
		fmt.Println("cannot print empty trie")
		return
	}
	fmt.Println(t.root.toString("", t.db))
}

// GetLastKey brings the key of right-most leaf node in the trie (jmlee)
func (t *Trie) GetLastKey() *big.Int {
	lastKey := t.getLastKey(t.root, nil)
	// fmt.Println("lastKey:", lastKey)
	return lastKey
}

// get last key among leaf nodes (i.e., right-most key value) (jmlee)
func (t *Trie) getLastKey(origNode node, lastKey []byte) *big.Int {
	switch n := (origNode).(type) {
	case nil:
		return big.NewInt(-1)
	case valueNode:
		hexToInt := new(big.Int)
		hexToInt.SetString(common.BytesToHash(hexToKeybytes(lastKey)).Hex()[2:], 16)
		// fmt.Println("lastkey:", lastKey, "len:", len(lastKey))
		// fmt.Println("hexToKeybytes(lastKey):", hexToKeybytes(lastKey), "len:", len(hexToKeybytes(lastKey)))
		return hexToInt
	case *shortNode:
		lastKey = append(lastKey, n.Key...)
		// fmt.Println("at getLastKey -> lastKey: ", lastKey, "/ appended key:", n.Key, " (short node)")
		return t.getLastKey(n.Val, lastKey)
	case *fullNode:
		last := 0
		for i, node := range &n.Children {
			if node != nil {
				// find last child index
				// this cannot be zeroHashNode since we deal with the corner case in the inactive trie
				last = i
			}
		}
		lastByte := common.HexToHash("0x" + indices[last])
		lastKey = append(lastKey, lastByte[len(lastByte)-1])
		// fmt.Println("at getLastKey -> lastKey: ", indices[last], "/ appended key:", indices[last], " (full node)")
		return t.getLastKey(n.Children[last], lastKey)
	case hashNode:
		child, err := t.resolveHash(n, nil)
		if err != nil {
			lastKey = nil
			return big.NewInt(-1)
		}
		return t.getLastKey(child, lastKey)
	default:
		panic(fmt.Sprintf("%T: invalid node: %v", origNode, origNode))
	}
}

// FindLeafNodes returns all leaf nodes within range (jmlee)
// ex. leafNodes, keys, err := normTrie.FindLeafNodes(common.HexToHash("0x0").Bytes(), common.HexToHash("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff").Bytes())
func (t *Trie) FindLeafNodes(startKey, endKey []byte) ([][]byte, []common.Hash, error) {
	// reset buffers
	accountsInTrie = make([][]byte, 0)
	keysToLeafNode = make([]common.Hash, 0)
	prefix := []byte{}

	// start iterating
	t.findLeafNodes(t.root, prefix, keybytesToHex(startKey), keybytesToHex(endKey))
	return accountsInTrie, keysToLeafNode, nil
}

func (t *Trie) findLeafNodes(n node, prefix, startKey, endKey []byte) {

	switch n := n.(type) {
	case nil:
		return

	case *shortNode:
		// update prefix & range
		prefix = append(prefix, n.Key...)
		prefixLen := len(prefix)
		subStartKey := startKey[:prefixLen]
		subEndKey := endKey[:prefixLen]

		// keep traversing if we are still in range (subStartKey <= prefix <= subEndKey)
		if bytes.Compare(subStartKey, prefix) <= 0 && bytes.Compare(subEndKey, prefix) >= 0 {
			t.findLeafNodes(n.Val, prefix, startKey, endKey)
		}

	case *fullNode:
		// update range
		prefixLen := len(prefix) + 1 // add 1 for fullNode's index
		subStartKey := startKey[:prefixLen]
		subEndKey := endKey[:prefixLen]

		for i, childNode := range &n.Children {
			if childNode != nil {
				// update prefix
				indexToByte := common.HexToHash("0x" + indices[i])
				extendedPrefix := append(prefix, indexToByte[len(indexToByte)-1])

				// keep traversing if we are still in range (subStartKey <= prefix <= subEndKey)
				if bytes.Compare(subStartKey, extendedPrefix) <= 0 && bytes.Compare(subEndKey, extendedPrefix) >= 0 {
					t.findLeafNodes(childNode, extendedPrefix, startKey, endKey)
				}
			}
		}

	case hashNode:
		rn, err := t.resolveHash(n, nil)
		if err != nil {
			panic("trie.findLeafNodes(): this node do not exist")
		}
		t.findLeafNodes(rn, prefix, startKey, endKey)

	case valueNode:
		// find leaf node within range
		accountsInTrie = append(accountsInTrie, n)
		keysToLeafNode = append(keysToLeafNode, common.BytesToHash(hexToKeybytes(prefix)))

	default:
		panic(fmt.Sprintf("%T: invalid node: %v", n, n))
	}
}

// get shortnode's size (for debugging)
func getShortnodeSize(n shortNode) int {
	h := newHasher(false)
	defer returnHasherToPool(h)
	collapsed, _ := h.hashShortNodeChildren(&n)
	h.tmp.Reset()
	if err := rlp.Encode(&h.tmp, collapsed); err != nil {
		panic("encode error: " + err.Error())
	}
	return len(h.tmp)
}

// TryDeleteLeft removes leftmost account if its key is less than or equal to endKey (for Ethane's inactivation)
// If a node was not found in the database, a MissingNodeError is returned.
func (t *Trie) TryDeleteLeft(endKey []byte) (error, []byte) {
	t.unhashed++
	dirty, n, err, enc := t.deleteLeft(t.root, nil, keybytesToHex(endKey))
	if !dirty || err != nil {
		// did not delete leftmost leaf node
		return err, nil
	}

	// delete leftmost leaf node
	t.root = n
	return nil, enc
}

// deleteLeft returns the new root of the trie with key deleted.
// It reduces the trie to minimal form by simplifying
// nodes on the way up after deleting recursively.
func (t *Trie) deleteLeft(n node, prefix, endKey []byte) (bool, node, error, []byte) {
	switch n := n.(type) {
	case *shortNode:
		prefix = append(prefix, n.Key...)
		// if n.Val == nil {
		// 	fmt.Println("short node value is nil")
		// }
		dirty, child, err, enc := t.deleteLeft(n.Val, prefix, endKey)
		if !dirty || err != nil {
			// fmt.Println("d1")
			return false, n, err, nil
		}

		switch child := child.(type) {
		case *shortNode:
			// Deleting from the subtrie reduced it to another
			// short node. Merge the nodes to avoid creating a
			// shortNode{..., shortNode{...}}. Use concat (which
			// always creates a new slice) instead of append to
			// avoid modifying n.Key since it might be shared with
			// other nodes.
			return true, &shortNode{concat(n.Key, child.Key...), child.Val, t.newFlag()}, nil, enc
		case nil:
			return true, nil, nil, enc // remove n entirely for whole matches
		default:
			return true, &shortNode{n.Key, child, t.newFlag()}, nil, enc
		}

	case *fullNode:
		// find leftmost node in this fullNode
		for i, childNode := range &n.Children {
			if childNode != nil {
				indexToByte := common.HexToHash("0x" + indices[i])
				prefix := append(prefix, indexToByte[len(indexToByte)-1])
				dirty, nn, err, enc := t.deleteLeft(childNode, prefix, endKey)
				if !dirty || err != nil {
					// fmt.Println("d2")
					return false, n, err, nil
				}
				n = n.copy()
				n.flags = t.newFlag()
				n.Children[i] = nn

				// Because n is a full node, it must've contained at least two children
				// before the delete operation. If the new child value is non-nil, n still
				// has at least two children after the deletion, and cannot be reduced to
				// a short node.
				if nn != nil {
					return true, n, nil, enc
				}
				// Reduction:
				// Check how many non-nil entries are left after deleting and
				// reduce the full node to a short node if only one entry is
				// left. Since n must've contained at least two children
				// before deletion (otherwise it would not be a full node) n
				// can never be reduced to nil.
				//
				// When the loop is done, pos contains the index of the single
				// value that is left in n or -2 if n contains at least two
				// values.
				pos := -1
				for i, cld := range &n.Children {
					if cld != nil {
						if pos == -1 {
							pos = i
						} else {
							pos = -2
							break
						}
					}
				}
				if pos >= 0 {
					if pos != 16 {
						// If the remaining entry is a short node, it replaces
						// n and its key gets the missing nibble tacked to the
						// front. This avoids creating an invalid
						// shortNode{..., shortNode{...}}.  Since the entry
						// might not be loaded yet, resolve it just for this
						// check.
						cnode, err := t.resolve(n.Children[pos], prefix)
						if err != nil {
							// fmt.Println("d3")
							return false, nil, err, nil
						}
						if cnode, ok := cnode.(*shortNode); ok {
							k := append([]byte{byte(pos)}, cnode.Key...)
							return true, &shortNode{k, cnode.Val, t.newFlag()}, nil, enc
						}
					}
					// Otherwise, n is replaced by a one-nibble short node
					// containing the child.
					return true, &shortNode{[]byte{byte(pos)}, n.Children[pos], t.newFlag()}, nil, enc
				}
				// n still contains at least two values and cannot be reduced.
				return true, n, nil, enc
			}
		}

		// should not reach here
		os.Exit(1)
		return false, n, nil, nil

	case valueNode:
		// fmt.Println("prefix:", prefix)
		// fmt.Println("endKey:", endKey)
		if bytes.Compare(prefix, endKey) <= 0 {
			// if prefix <= endKey, delete this leaf node and return this valueNode
			// fmt.Println("do delete")
			return true, nil, nil, n
		} else {
			// this leaf node is out of range, do not this node
			// fmt.Println("not delete")
			return false, nil, nil, nil
		}

	case nil:
		// fmt.Println("d4")
		return false, nil, nil, nil

	case hashNode:
		// We've hit a part of the trie that isn't loaded yet. Load
		// the node and delete from it. This leaves all child nodes on
		// the path to the value in the trie.
		rn, err := t.resolveHash(n, prefix)
		if err != nil {
			// fmt.Println("d5")
			return false, nil, err, nil
		}
		dirty, nn, err, enc := t.deleteLeft(rn, prefix, endKey)
		if !dirty || err != nil {
			// fmt.Println("d6")
			return false, rn, err, enc
		}
		return true, nn, nil, enc

	default:
		panic(fmt.Sprintf("%T: invalid node: %v (%v)", n, n, endKey))
	}
}
