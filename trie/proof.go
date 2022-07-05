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
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

// (joonha) TODO: 이거 사용되는지 확인하고, 아니면 지우기
// type ProofList [][]byte // for external use
type ProofList common.ProofList

// (joonha)
func (n *ProofList) Has(key []byte) (bool, error) {
	panic("not supported")
}

// (joonha)
func (n *ProofList) Get(key []byte) ([]byte, error) {
	x := (*n)[0]
	*n = (*n)[1:]
	return x, nil
}

// Prove constructs a merkle proof for key. The result contains all encoded nodes
// on the path to the value at key. The value itself is also included in the last
// node and can be retrieved by verifying the proof.
//
// If the trie does not contain a value for key, the returned proof contains all
// nodes of the longest existing prefix of the key (at least the root node), ending
// with the node that proves the absence of the key.
func (t *Trie) Prove(key []byte, fromLevel uint, proofDb ethdb.KeyValueWriter) error {
	// Collect all nodes on the path to key.
	key = keybytesToHex(key)
	var nodes []node
	tn := t.root
	for len(key) > 0 && tn != nil {
		switch n := tn.(type) {
		case *shortNode:
			if len(key) < len(n.Key) || !bytes.Equal(n.Key, key[:len(n.Key)]) {
				// The trie doesn't contain the key.
				tn = nil
			} else {
				tn = n.Val
				key = key[len(n.Key):]
			}
			nodes = append(nodes, n)
		case *fullNode:
			tn = n.Children[key[0]]
			key = key[1:]
			nodes = append(nodes, n)
		case hashNode:
			var err error
			tn, err = t.resolveHash(n, nil)
			if err != nil {
				log.Error(fmt.Sprintf("Unhandled trie error: %v", err))
				return err
			}
		default:
			panic(fmt.Sprintf("%T: invalid node: %v", tn, tn))
		}
	}
	hasher := newHasher(false)
	defer returnHasherToPool(hasher)

	for i, n := range nodes {
		if fromLevel > 0 {
			fromLevel--
			continue
		}
		var hn node
		n, hn = hasher.proofHash(n)
		if hash, ok := hn.(hashNode); ok || i == 0 {
			// If the node's database encoding is a hash (or is the
			// root node), it becomes a proof element.
			enc, _ := rlp.EncodeToBytes(n)
			if !ok {
				hash = hasher.hashData(enc)
			}
			proofDb.Put(hash, enc)
		}
	}
	return nil
}

// Prove constructs a merkle proof for key. The result contains all encoded nodes
// on the path to the value at key. The value itself is also included in the last
// node and can be retrieved by verifying the proof.
//
// If the trie does not contain a value for key, the returned proof contains all
// nodes of the longest existing prefix of the key (at least the root node), ending
// with the node that proves the absence of the key.
func (t *SecureTrie) Prove(key []byte, fromLevel uint, proofDb ethdb.KeyValueWriter) error {
	return t.trie.Prove(key, fromLevel, proofDb)
}

// VerifyProof checks merkle proofs. The given proof must contain the value for
// key in a trie with the given root hash. VerifyProof returns an error if the
// proof contains invalid trie nodes or the wrong value.
func VerifyProof(rootHash common.Hash, key []byte, proofDb ethdb.KeyValueReader) (value []byte, err error) {
	key = keybytesToHex(key)
	wantHash := rootHash
	for i := 0; ; i++ {
		buf, _ := proofDb.Get(wantHash[:])
		if buf == nil {
			return nil, fmt.Errorf("proof node %d (hash %064x) missing", i, wantHash)
		}
		n, err := decodeNode(wantHash[:], buf)
		if err != nil {
			return nil, fmt.Errorf("bad proof node %d: %v", i, err)
		}
		// get returns the child's key of the node n (joonha)
		keyrest, cld := get(n, key, true)
		switch cld := cld.(type) {
		case nil:
			// The trie doesn't contain the key.
			return nil, nil
		case hashNode:
			key = keyrest
			copy(wantHash[:], cld)
		case valueNode:
			return cld, nil
		}
	}
}

// Only difference with VerifyProof is proofDb's type (joonha)
func VerifyProof_ProofList(rootHash common.Hash, key []byte, proofDb common.ProofList) (value []byte, err error) {
	key = keybytesToHex(key)
	wantHash := rootHash
	for i := 0; ; i++ {
		buf, _ := proofDb.Get(wantHash[:])
		if buf == nil {
			return nil, fmt.Errorf("proof node %d (hash %064x) missing", i, wantHash)
		}
		n, err := decodeNode(wantHash[:], buf)
		if err != nil {
			return nil, fmt.Errorf("bad proof node %d: %v", i, err)
		}
		// get returns the child's key of the node n (joonha)
		keyrest, cld := get(n, key, true)
		switch cld := cld.(type) {
		case nil:
			// The trie doesn't contain the key.
			return nil, nil
		case hashNode:
			key = keyrest
			copy(wantHash[:], cld)
		case valueNode:
			return cld, nil
		}
	}
}

// optimized above proving function to compare only the top node of the merkleProof and the blockRoot (joonha)
func VerifyProof_restore(rootHash common.Hash, proofDb common.ProofList) (value []byte, err error) {
	wantHash := rootHash

	buf, _ := proofDb.Get(wantHash[:])
	if buf == nil {
		return nil, fmt.Errorf("proof node (hash %064x) missing", wantHash)
	}
	_, err = decodeNode(wantHash[:], buf)
	if err != nil {
		return nil, fmt.Errorf("bad proof node: %v", err)
	}
	return nil, err
}

// GetKeyFromMerkleProof returns the leaf accounts and its keys from merkle proofs (joonha)
func GetKeyFromMerkleProof(rootHash common.Hash, proofDb common.ProofList) ([][]byte, []common.Hash) {

	var targetNodes [][]byte
	var retrievedKeys []common.Hash

	// proofDb는 현재 여러 MP가 뭉쳐있는 것임.
	// 이를 [64 0]을 기준으로 잘라주면 될 것 같음.
	// 자른 proofDb마다 getKeyFromMerkleProof를 호출하면 될 것.

	dummy_root := []byte{'@', 0}
	// fmt.Println("dummy_root: ", dummy_root)
	// fmt.Println("\n\nproofDb ===>")
	// fmt.Println(proofDb, "\n===> end of proofDb\n\n")
	// fmt.Println("proofDb length is ", len(proofDb))
	// fmt.Println("\n\nproofDb[1:3] is", proofDb[1:3])
	
	start_idx := 0
	for i := 0; i < len(proofDb); i++ {
		if bytes.Equal(proofDb[i], dummy_root) == true {
			// current i is a new start of the MP
			// start_idx ~ i까지를 잘라서 함수 호출하면 될 듯.
			// start iteration from the root node

			// replace dummy_nodes with original ref nodes
			for j := start_idx; j < i; j++ {
				if proofDb[j][0] == '@' {
					ref_idx := proofDb[j][1]
					proofDb[j] = proofDb[ref_idx]
				}
			}

			targetNode, tKey := getKeyFromMerkleProof(rootHash, nil, nil, proofDb[start_idx:i])

			start_idx = i

			// convert to a key format (hash)
			tKey_i := tKey.Int64() // big.Int -> int64
			retrievedKey := common.HexToHash(strconv.FormatInt(tKey_i, 16)) // int64 -> hex -> hash
			// fmt.Println("flag 3")
			// fmt.Println("\n\ntargetNode ===>")
			// fmt.Println(targetNode, "\n===> end of targetNode\n")
			// fmt.Println("retrievedKey ===>")
			// fmt.Println(retrievedKey, "\n===> end of retrievedKey\n\n")

			targetNodes = append(targetNodes, targetNode)
			retrievedKeys = append(retrievedKeys, retrievedKey)
		}
	}

	/* for last MP */
	for j := start_idx; j < len(proofDb); j++ {
		if proofDb[j][0] == '@' {
			ref_idx := proofDb[j][1]
			proofDb[j] = proofDb[ref_idx]
		}
	}

	// start iteration from the root node
	// targetNode, tKey := getKeyFromMerkleProof(rootHash, nil, nil, proofDb)
	targetNode, tKey := getKeyFromMerkleProof(rootHash, nil, nil, proofDb[start_idx:])

	// convert to a key format (hash)
	tKey_i := tKey.Int64() // big.Int -> int64
	retrievedKey := common.HexToHash(strconv.FormatInt(tKey_i, 16)) // int64 -> hex -> hash

	// fmt.Println("flag 4")
	// fmt.Println("\n\ntargetNode ===>")
	// fmt.Println(targetNode, "\n===> end of targetNode\n")
	// fmt.Println("retrievedKey ===>")
	// fmt.Println(retrievedKey, "\n===> end of retrievedKey\n\n")

	targetNodes = append(targetNodes, targetNode)
	retrievedKeys = append(retrievedKeys, retrievedKey)

	return targetNodes, retrievedKeys
}

// internal function of GetKeyFromMerkleProof (joonha)
func getKeyFromMerkleProof(nodeHash common.Hash, origNode node, tKey []byte, proofDb common.ProofList) ([]byte, *big.Int) {

	// fmt.Println("\n=============================================================================")
	// fmt.Println("=============================================================================\n")

	/*************************************************************/
	// README
	/*************************************************************/
	// There is NO HASHNODE in the proofDb buffer (just short, full, and value)
	// nodeHash: for retrieving a currNode
	// origNode: previous node
	// tKey: target Key that may be complete reaching a value node
	// proofDb: merkle proof stream
	// At first, origNode is nil.
	//
	// Last shortNode's child is a valueNode, the target account we want to retrieve.


	/*************************************************************/
	// TOOLS
	/*************************************************************/
	// resolveNode retrieves and resolves trie node from merkle proof stream
	resolveNode := func(hash common.Hash) (node, error) {
		buf, _ := proofDb.Get(hash[:])
		if buf == nil {
			return nil, fmt.Errorf("proof node (hash %064x) missing", hash)
		}
		n, err := decodeNode(hash[:], buf)
		if err != nil {
			return nil, fmt.Errorf("bad proof node %v", err)
		}
		return n, err
	}


	/*************************************************************/
	// GET CURRENT NODE
	/*************************************************************/
	// get a node from proofDb representing the nodeHash

	// fmt.Println("proofDb: ", proofDb)

	// Reaching the END
	if len(proofDb) == 0 { // Reaching the valueNode, return.
		// fmt.Println("proofDb is nil") 

		hexToInt := new(big.Int)

		// // fmt.Println("tKey: ", tKey)
		// // fmt.Println("len(tKey): ", len(tKey))
		// if len(tKey) % 2 == 0 {
		// 	// 앞에 0을 추가하자.
		// 	// fmt.Println("length is even!")
		// 	tKey = append([]byte{0}, tKey...)
		// 	// fmt.Println("tKey: ", tKey)
		// 	// fmt.Println("len(tKey): ", len(tKey))
		// 	hexToInt.SetString(common.BytesToHash(hexToKeybytes(tKey)).Hex()[2:], 16) /////////// panic: can't convert hex key of odd length
		// } else {
		// 	hexToInt.SetString(common.BytesToHash(hexToKeybytes(tKey)).Hex()[2:], 16) /////////// panic: can't convert hex key of odd length
		// }
		hexToInt.SetString(common.BytesToHash(hexToKeybytes(tKey)).Hex()[2:], 16) /////////// panic: can't convert hex key of odd length

		// get the target valueNode
		for i := 0; ; i++ {
			_, cld := get(origNode, tKey[len(tKey)-1:], true)
			// fmt.Println("tKey is ", tKey[len(tKey)-1:])
			switch cld := cld.(type) {
			case nil:
				// The trie doesn't contain the key.
				// fmt.Println("nil")
				return nil, hexToInt
			case hashNode:
				copy(nodeHash[:], cld)
				// fmt.Println("hashNode")
			case valueNode:
				// fmt.Println("valueNode")
				return cld, hexToInt
			default:
				// fmt.Println("default")
			}
		}
		return nil, hexToInt

	} else {
		// fmt.Println("proofDb is not nil")
	}

	// Not reaching the end. Go more.
	currNode, err := resolveNode(nodeHash)
	if err != nil {
		fmt.Errorf("bad proof node: %v", err)
		return nil, nil
	} 


	/*************************************************************/
	// PREVIOUS NODE
	/*************************************************************/
	// if previous node(=origNode) is a fullNode, 
	// extract key digit and append it to tKey
	switch n := (origNode).(type) {
	case *fullNode:
		// fmt.Println("Prev: full")

		switch cur := (currNode).(type){
		case *fullNode:
			hasher := newHasher(false)
			defer returnHasherToPool(hasher)
			nn := hasher.fullnodeToHash(cur, false)
			// fmt.Println("selected branch: ", nn)

			i := 0
			for i < 16 {
				if common.BytesToHash(nn.(hashNode)) == common.BytesToHash((n.Children[i]).(hashNode)) {
					// key append
					selectedByte := common.HexToHash("0x" + indices[i])
					tKey = append(tKey, selectedByte[len(selectedByte)-1])
					break
				}
				i++
			}
		case *shortNode:
			hasher := newHasher(false)
			defer returnHasherToPool(hasher)

			collapsed, _ := hasher.hashShortNodeChildren(cur) // should hash the child valueNode first
			nn := hasher.shortnodeToHash(collapsed, false) // nn: hashnode
			// fmt.Println("selected branch: ", nn)

			i := 15 // from the latest
			for i >= 0 {
				// fmt.Println("common.BytesToHash(nn.(hashNode)): ", common.BytesToHash(nn.(hashNode)))
				// to avoid panic
				if n.Children[i] == nil {
					i--
					continue
				}

				// if it is already used, continue to search another inactive account


				// fmt.Println("common.BytesToHash((n.Children[i]).(hashNode)): ", common.BytesToHash((n.Children[i]).(hashNode)))
				if common.BytesToHash(nn.(hashNode)) == common.BytesToHash((n.Children[i]).(hashNode)) {

					selectedByte := common.HexToHash("0x" + indices[i])

					// if this is already used, continue to search another inactive account during the epoch
					// TODO(joonha): simplify this code part
					tKey_tmp := tKey
					tKey_tmp = append(tKey_tmp, selectedByte[len(selectedByte)-1])
					hexToInt_tmp := new(big.Int)
					hexToInt_tmp.SetString(common.BytesToHash(hexToKeybytes(tKey_tmp)).Hex()[2:], 16)
					tKey_i := hexToInt_tmp.Int64() // big.Int -> int64
					retrievedKey_tmp := common.HexToHash(strconv.FormatInt(tKey_i, 16)) // int64 -> hex -> hash
					_, doExist := common.AlreadyRestored[retrievedKey_tmp]
					if doExist { // already restored
						// fmt.Println("ALREADY RESTORED, so continue to search")
						i--
						continue
					}

					// key append
					// selectedByte := common.HexToHash("0x" + indices[i])
					tKey = append(tKey, selectedByte[len(selectedByte)-1])
					break
				}
				i--
			}
		}

	default:
		// fmt.Println("Prev: not full")
	}


	/*************************************************************/
	// CURRENT NODE
	/*************************************************************/
	switch n := (currNode).(type) { // no valueNode and hashNode case (only shortNode and fullNode)
	case nil:
		// fmt.Println("curr nil")
		return nil, big.NewInt(0)

	case *shortNode: // should update the key.
		// fmt.Println("curr short")
		// fmt.Println("n.Key is ", n.Key)
		// fmt.Println("before tKey: ", tKey)
		tKey = append(tKey, n.Key...)
		// fmt.Println("after tKey: ", tKey)

		return getKeyFromMerkleProof(nodeHash, n, tKey, proofDb)

	case *fullNode: // No key update. It is the next node's duty.
		// fmt.Println("curr full")
		// fmt.Println("full node's children: ", n.Children)
		return getKeyFromMerkleProof(nodeHash, n, tKey, proofDb)

	case hashNode: // there would be no hashNode in proofDb
		// fmt.Println("curr hash")		
		return getKeyFromMerkleProof(nodeHash, n, tKey, proofDb)

	default:
		panic(fmt.Sprintf("%T: invalid node: %v", origNode, origNode))
	}
}

// proofToPath converts a merkle proof to trie node path. The main purpose of
// this function is recovering a node path from the merkle proof stream. All
// necessary nodes will be resolved and leave the remaining as hashnode.
//
// The given edge proof is allowed to be an existent or non-existent proof.
func proofToPath(rootHash common.Hash, root node, key []byte, proofDb ethdb.KeyValueReader, allowNonExistent bool) (node, []byte, error) {
	// resolveNode retrieves and resolves trie node from merkle proof stream
	resolveNode := func(hash common.Hash) (node, error) {
		buf, _ := proofDb.Get(hash[:])
		if buf == nil {
			return nil, fmt.Errorf("proof node (hash %064x) missing", hash)
		}
		n, err := decodeNode(hash[:], buf)
		if err != nil {
			return nil, fmt.Errorf("bad proof node %v", err)
		}
		return n, err
	}
	// If the root node is empty, resolve it first.
	// Root node must be included in the proof.
	if root == nil {
		n, err := resolveNode(rootHash)
		if err != nil {
			return nil, nil, err
		}
		root = n
	}
	var (
		err           error
		child, parent node
		keyrest       []byte
		valnode       []byte
	)
	key, parent = keybytesToHex(key), root
	for {
		keyrest, child = get(parent, key, false)
		switch cld := child.(type) {
		case nil:
			// The trie doesn't contain the key. It's possible
			// the proof is a non-existing proof, but at least
			// we can prove all resolved nodes are correct, it's
			// enough for us to prove range.
			if allowNonExistent {
				return root, nil, nil
			}
			return nil, nil, errors.New("the node is not contained in trie")
		case *shortNode:
			key, parent = keyrest, child // Already resolved
			continue
		case *fullNode:
			key, parent = keyrest, child // Already resolved
			continue
		case hashNode:
			child, err = resolveNode(common.BytesToHash(cld))
			if err != nil {
				return nil, nil, err
			}
		case valueNode:
			valnode = cld
		}
		// Link the parent and child.
		switch pnode := parent.(type) {
		case *shortNode:
			pnode.Val = child
		case *fullNode:
			pnode.Children[key[0]] = child
		default:
			panic(fmt.Sprintf("%T: invalid node: %v", pnode, pnode))
		}
		if len(valnode) > 0 {
			return root, valnode, nil // The whole path is resolved
		}
		key, parent = keyrest, child
	}
}

// unsetInternal removes all internal node references(hashnode, embedded node).
// It should be called after a trie is constructed with two edge paths. Also
// the given boundary keys must be the one used to construct the edge paths.
//
// It's the key step for range proof. All visited nodes should be marked dirty
// since the node content might be modified. Besides it can happen that some
// fullnodes only have one child which is disallowed. But if the proof is valid,
// the missing children will be filled, otherwise it will be thrown anyway.
//
// Note we have the assumption here the given boundary keys are different
// and right is larger than left.
func unsetInternal(n node, left []byte, right []byte) (bool, error) {
	left, right = keybytesToHex(left), keybytesToHex(right)

	// Step down to the fork point. There are two scenarios can happen:
	// - the fork point is a shortnode: either the key of left proof or
	//   right proof doesn't match with shortnode's key.
	// - the fork point is a fullnode: both two edge proofs are allowed
	//   to point to a non-existent key.
	var (
		pos    = 0
		parent node

		// fork indicator, 0 means no fork, -1 means proof is less, 1 means proof is greater
		shortForkLeft, shortForkRight int
	)
findFork:
	for {
		switch rn := (n).(type) {
		case *shortNode:
			rn.flags = nodeFlag{dirty: true}

			// If either the key of left proof or right proof doesn't match with
			// shortnode, stop here and the forkpoint is the shortnode.
			if len(left)-pos < len(rn.Key) {
				shortForkLeft = bytes.Compare(left[pos:], rn.Key)
			} else {
				shortForkLeft = bytes.Compare(left[pos:pos+len(rn.Key)], rn.Key)
			}
			if len(right)-pos < len(rn.Key) {
				shortForkRight = bytes.Compare(right[pos:], rn.Key)
			} else {
				shortForkRight = bytes.Compare(right[pos:pos+len(rn.Key)], rn.Key)
			}
			if shortForkLeft != 0 || shortForkRight != 0 {
				break findFork
			}
			parent = n
			n, pos = rn.Val, pos+len(rn.Key)
		case *fullNode:
			rn.flags = nodeFlag{dirty: true}

			// If either the node pointed by left proof or right proof is nil,
			// stop here and the forkpoint is the fullnode.
			leftnode, rightnode := rn.Children[left[pos]], rn.Children[right[pos]]
			if leftnode == nil || rightnode == nil || leftnode != rightnode {
				break findFork
			}
			parent = n
			n, pos = rn.Children[left[pos]], pos+1
		default:
			panic(fmt.Sprintf("%T: invalid node: %v", n, n))
		}
	}
	switch rn := n.(type) {
	case *shortNode:
		// There can have these five scenarios:
		// - both proofs are less than the trie path => no valid range
		// - both proofs are greater than the trie path => no valid range
		// - left proof is less and right proof is greater => valid range, unset the shortnode entirely
		// - left proof points to the shortnode, but right proof is greater
		// - right proof points to the shortnode, but left proof is less
		if shortForkLeft == -1 && shortForkRight == -1 {
			return false, errors.New("empty range")
		}
		if shortForkLeft == 1 && shortForkRight == 1 {
			return false, errors.New("empty range")
		}
		if shortForkLeft != 0 && shortForkRight != 0 {
			// The fork point is root node, unset the entire trie
			if parent == nil {
				return true, nil
			}
			parent.(*fullNode).Children[left[pos-1]] = nil
			return false, nil
		}
		// Only one proof points to non-existent key.
		if shortForkRight != 0 {
			if _, ok := rn.Val.(valueNode); ok {
				// The fork point is root node, unset the entire trie
				if parent == nil {
					return true, nil
				}
				parent.(*fullNode).Children[left[pos-1]] = nil
				return false, nil
			}
			return false, unset(rn, rn.Val, left[pos:], len(rn.Key), false)
		}
		if shortForkLeft != 0 {
			if _, ok := rn.Val.(valueNode); ok {
				// The fork point is root node, unset the entire trie
				if parent == nil {
					return true, nil
				}
				parent.(*fullNode).Children[right[pos-1]] = nil
				return false, nil
			}
			return false, unset(rn, rn.Val, right[pos:], len(rn.Key), true)
		}
		return false, nil
	case *fullNode:
		// unset all internal nodes in the forkpoint
		for i := left[pos] + 1; i < right[pos]; i++ {
			rn.Children[i] = nil
		}
		if err := unset(rn, rn.Children[left[pos]], left[pos:], 1, false); err != nil {
			return false, err
		}
		if err := unset(rn, rn.Children[right[pos]], right[pos:], 1, true); err != nil {
			return false, err
		}
		return false, nil
	default:
		panic(fmt.Sprintf("%T: invalid node: %v", n, n))
	}
}

// unset removes all internal node references either the left most or right most.
// It can meet these scenarios:
//
// - The given path is existent in the trie, unset the associated nodes with the
//   specific direction
// - The given path is non-existent in the trie
//   - the fork point is a fullnode, the corresponding child pointed by path
//     is nil, return
//   - the fork point is a shortnode, the shortnode is included in the range,
//     keep the entire branch and return.
//   - the fork point is a shortnode, the shortnode is excluded in the range,
//     unset the entire branch.
func unset(parent node, child node, key []byte, pos int, removeLeft bool) error {
	switch cld := child.(type) {
	case *fullNode:
		if removeLeft {
			for i := 0; i < int(key[pos]); i++ {
				cld.Children[i] = nil
			}
			cld.flags = nodeFlag{dirty: true}
		} else {
			for i := key[pos] + 1; i < 16; i++ {
				cld.Children[i] = nil
			}
			cld.flags = nodeFlag{dirty: true}
		}
		return unset(cld, cld.Children[key[pos]], key, pos+1, removeLeft)
	case *shortNode:
		if len(key[pos:]) < len(cld.Key) || !bytes.Equal(cld.Key, key[pos:pos+len(cld.Key)]) {
			// Find the fork point, it's an non-existent branch.
			if removeLeft {
				if bytes.Compare(cld.Key, key[pos:]) < 0 {
					// The key of fork shortnode is less than the path
					// (it belongs to the range), unset the entrie
					// branch. The parent must be a fullnode.
					fn := parent.(*fullNode)
					fn.Children[key[pos-1]] = nil
				} else {
					// The key of fork shortnode is greater than the
					// path(it doesn't belong to the range), keep
					// it with the cached hash available.
				}
			} else {
				if bytes.Compare(cld.Key, key[pos:]) > 0 {
					// The key of fork shortnode is greater than the
					// path(it belongs to the range), unset the entrie
					// branch. The parent must be a fullnode.
					fn := parent.(*fullNode)
					fn.Children[key[pos-1]] = nil
				} else {
					// The key of fork shortnode is less than the
					// path(it doesn't belong to the range), keep
					// it with the cached hash available.
				}
			}
			return nil
		}
		if _, ok := cld.Val.(valueNode); ok {
			fn := parent.(*fullNode)
			fn.Children[key[pos-1]] = nil
			return nil
		}
		cld.flags = nodeFlag{dirty: true}
		return unset(cld, cld.Val, key, pos+len(cld.Key), removeLeft)
	case nil:
		// If the node is nil, then it's a child of the fork point
		// fullnode(it's a non-existent branch).
		return nil
	default:
		panic("it shouldn't happen") // hashNode, valueNode
	}
}

// hasRightElement returns the indicator whether there exists more elements
// on the right side of the given path. The given path can point to an existent
// key or a non-existent one. This function has the assumption that the whole
// path should already be resolved.
func hasRightElement(node node, key []byte) bool {
	pos, key := 0, keybytesToHex(key)
	for node != nil {
		switch rn := node.(type) {
		case *fullNode:
			for i := key[pos] + 1; i < 16; i++ {
				if rn.Children[i] != nil {
					return true
				}
			}
			node, pos = rn.Children[key[pos]], pos+1
		case *shortNode:
			if len(key)-pos < len(rn.Key) || !bytes.Equal(rn.Key, key[pos:pos+len(rn.Key)]) {
				return bytes.Compare(rn.Key, key[pos:]) > 0
			}
			node, pos = rn.Val, pos+len(rn.Key)
		case valueNode:
			return false // We have resolved the whole path
		default:
			panic(fmt.Sprintf("%T: invalid node: %v", node, node)) // hashnode
		}
	}
	return false
}

// VerifyRangeProof checks whether the given leaf nodes and edge proof
// can prove the given trie leaves range is matched with the specific root.
// Besides, the range should be consecutive (no gap inside) and monotonic
// increasing.
//
// Note the given proof actually contains two edge proofs. Both of them can
// be non-existent proofs. For example the first proof is for a non-existent
// key 0x03, the last proof is for a non-existent key 0x10. The given batch
// leaves are [0x04, 0x05, .. 0x09]. It's still feasible to prove the given
// batch is valid.
//
// The firstKey is paired with firstProof, not necessarily the same as keys[0]
// (unless firstProof is an existent proof). Similarly, lastKey and lastProof
// are paired.
//
// Expect the normal case, this function can also be used to verify the following
// range proofs:
//
// - All elements proof. In this case the proof can be nil, but the range should
//   be all the leaves in the trie.
//
// - One element proof. In this case no matter the edge proof is a non-existent
//   proof or not, we can always verify the correctness of the proof.
//
// - Zero element proof. In this case a single non-existent proof is enough to prove.
//   Besides, if there are still some other leaves available on the right side, then
//   an error will be returned.
//
// Except returning the error to indicate the proof is valid or not, the function will
// also return a flag to indicate whether there exists more accounts/slots in the trie.
//
// Note: This method does not verify that the proof is of minimal form. If the input
// proofs are 'bloated' with neighbour leaves or random data, aside from the 'useful'
// data, then the proof will still be accepted.
func VerifyRangeProof(rootHash common.Hash, firstKey []byte, lastKey []byte, keys [][]byte, values [][]byte, proof ethdb.KeyValueReader) (bool, error) {
	if len(keys) != len(values) {
		return false, fmt.Errorf("inconsistent proof data, keys: %d, values: %d", len(keys), len(values))
	}
	// Ensure the received batch is monotonic increasing and contains no deletions
	for i := 0; i < len(keys)-1; i++ {
		if bytes.Compare(keys[i], keys[i+1]) >= 0 {
			return false, errors.New("range is not monotonically increasing")
		}
	}
	for _, value := range values {
		if len(value) == 0 {
			return false, errors.New("range contains deletion")
		}
	}
	// Special case, there is no edge proof at all. The given range is expected
	// to be the whole leaf-set in the trie.
	if proof == nil {
		tr := NewStackTrie(nil)
		for index, key := range keys {
			tr.TryUpdate(key, values[index])
		}
		if have, want := tr.Hash(), rootHash; have != want {
			return false, fmt.Errorf("invalid proof, want hash %x, got %x", want, have)
		}
		return false, nil // No more elements
	}
	// Special case, there is a provided edge proof but zero key/value
	// pairs, ensure there are no more accounts / slots in the trie.
	if len(keys) == 0 {
		root, val, err := proofToPath(rootHash, nil, firstKey, proof, true)
		if err != nil {
			return false, err
		}
		if val != nil || hasRightElement(root, firstKey) {
			return false, errors.New("more entries available")
		}
		return false, nil
	}
	// Special case, there is only one element and two edge keys are same.
	// In this case, we can't construct two edge paths. So handle it here.
	if len(keys) == 1 && bytes.Equal(firstKey, lastKey) {
		root, val, err := proofToPath(rootHash, nil, firstKey, proof, false)
		if err != nil {
			return false, err
		}
		if !bytes.Equal(firstKey, keys[0]) {
			return false, errors.New("correct proof but invalid key")
		}
		if !bytes.Equal(val, values[0]) {
			return false, errors.New("correct proof but invalid data")
		}
		return hasRightElement(root, firstKey), nil
	}
	// Ok, in all other cases, we require two edge paths available.
	// First check the validity of edge keys.
	if bytes.Compare(firstKey, lastKey) >= 0 {
		return false, errors.New("invalid edge keys")
	}
	// todo(rjl493456442) different length edge keys should be supported
	if len(firstKey) != len(lastKey) {
		return false, errors.New("inconsistent edge keys")
	}
	// Convert the edge proofs to edge trie paths. Then we can
	// have the same tree architecture with the original one.
	// For the first edge proof, non-existent proof is allowed.
	root, _, err := proofToPath(rootHash, nil, firstKey, proof, true)
	if err != nil {
		return false, err
	}
	// Pass the root node here, the second path will be merged
	// with the first one. For the last edge proof, non-existent
	// proof is also allowed.
	root, _, err = proofToPath(rootHash, root, lastKey, proof, true)
	if err != nil {
		return false, err
	}
	// Remove all internal references. All the removed parts should
	// be re-filled(or re-constructed) by the given leaves range.
	empty, err := unsetInternal(root, firstKey, lastKey)
	if err != nil {
		return false, err
	}
	// Rebuild the trie with the leaf stream, the shape of trie
	// should be same with the original one.
	tr := &Trie{root: root, db: NewDatabase(memorydb.New())}
	if empty {
		tr.root = nil
	}
	for index, key := range keys {
		tr.TryUpdate(key, values[index])
	}
	if tr.Hash() != rootHash {
		return false, fmt.Errorf("invalid proof, want hash %x, got %x", rootHash, tr.Hash())
	}
	return hasRightElement(tr.root, keys[len(keys)-1]), nil
}

// get returns the child of the given node. Return nil if the
// node with specified key doesn't exist at all.
//
// There is an additional flag `skipResolved`. If it's set then
// all resolved nodes won't be returned.
func get(tn node, key []byte, skipResolved bool) ([]byte, node) {
	for {
		switch n := tn.(type) {
		case *shortNode:
			if len(key) < len(n.Key) || !bytes.Equal(n.Key, key[:len(n.Key)]) {
				return nil, nil
			}
			tn = n.Val
			key = key[len(n.Key):]
			if !skipResolved {
				return key, tn
			}
		case *fullNode:
			tn = n.Children[key[0]]
			key = key[1:]
			if !skipResolved {
				return key, tn
			}
		case hashNode:
			return key, n
		case nil:
			return key, nil
		case valueNode:
			return nil, n
		default:
			panic(fmt.Sprintf("%T: invalid node: %v", tn, tn))
		}
	}
}
