// Copyright 2019 The go-ethereum Authors
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
	"fmt"
	"os"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"golang.org/x/crypto/sha3"
)

type sliceBuffer []byte

func (b *sliceBuffer) Write(data []byte) (n int, err error) {
	*b = append(*b, data...)
	return len(data), nil
}

func (b *sliceBuffer) Reset() {
	*b = (*b)[:0]
}

// hasher is a type used for the trie Hash operation. A hasher has some
// internal preallocated temp space
type hasher struct {
	sha      crypto.KeccakState
	tmp      sliceBuffer
	parallel bool // Whether to use paralallel threads when hashing
}

// hasherPool holds pureHashers
var hasherPool = sync.Pool{
	New: func() interface{} {
		return &hasher{
			tmp: make(sliceBuffer, 0, 550), // cap is as large as a full fullNode.
			sha: sha3.NewLegacyKeccak256().(crypto.KeccakState),
		}
	},
}

// (jmlee)
var HasherPool = hasherPool

func newHasher(parallel bool) *hasher {
	h := hasherPool.Get().(*hasher)
	h.parallel = parallel
	return h
}

// (jmlee)
func NewHasher(parallel bool) *hasher {
	h := hasherPool.Get().(*hasher)
	h.parallel = parallel
	return h
}

func returnHasherToPool(h *hasher) {
	hasherPool.Put(h)
}

// (jmlee)
func (h *hasher) Sha() crypto.KeccakState {
	return h.sha
}

// hash collapses a node down into a hash node, also returning a copy of the
// original node initialized with the computed hash to replace the original one.
func (h *hasher) hash(n node, force bool) (hashed node, cached node) {
	// Return the cached hash if it's available
	if hash, _ := n.cache(); hash != nil {
		return hash, n
	}
	// Trie not processed yet, walk the children
	switch n := n.(type) {
	case *shortNode:
		collapsed, cached := h.hashShortNodeChildren(n)
		hashed, size := h.shortnodeToHash(collapsed, force)
		// We need to retain the possibly _not_ hashed node, in case it was too
		// small to be hashed
		if hn, ok := hashed.(hashNode); ok {
			cached.flags.hash = hn

			// record trie node connection: store child node hashes (jmlee)
			myHash := common.BytesToHash(hn)
			common.ChildHashesMutex.Lock()
			if nodeInfo, exist := common.TrieNodeInfosDirty[myHash]; !exist {
				nodeInfo.IsShortNode = true
				nodeInfo.Size = 32 + uint(size)
				if n.Key[len(n.Key)-1] == 16 {
					nodeInfo.IsLeafNode = true
				}

				// set nodeInfo.Key
				var buffer bytes.Buffer
				for i := 0; i < len(n.Key); i++ {
					buffer.WriteString(Indices[n.Key[i]]) // faster than string += operation
				}
				nodeInfo.Key = buffer.String()
				// fmt.Println("nodeInfo.Key:", nodeInfo.Key, "/ len:", len(nodeInfo.Key))
				// fmt.Println("n.Key:", n.Key, "/ len:", len(n.Key))

				// set child node hashes
				switch v := cached.Val.(type) {
				case hashNode:
					childHash := common.BytesToHash(v)
					nodeInfo.ChildHashes = append(nodeInfo.ChildHashes, childHash)
					// fmt.Println("  hasher.go case hashNode")
					// fmt.Println("    short node hash:", myHash.Hex(), " / child fullNode hash:", childHash.Hex())
				case *fullNode:
					childHashNode, _ := v.cache()
					childHash := common.BytesToHash(childHashNode)
					nodeInfo.ChildHashes = append(nodeInfo.ChildHashes, childHash)
					// fmt.Println("  hasher.go case *fullNode")
					// fmt.Println("    short node hash:", myHash.Hex(), " / child fullNode hash:", childHash.Hex())
				case valueNode:
					// this short node is leaf node, so do not have child node
				case *shortNode:
					// short node's child cannot be short node
				default:
					fmt.Println("  hasher.go case default")
					fmt.Println("this should not happen")
					os.Exit(1)
				}

				common.TrieNodeInfosDirty[myHash] = nodeInfo
			}
			common.ChildHashesMutex.Unlock()
		} else {
			cached.flags.hash = nil
		}
		return hashed, cached
	case *fullNode:
		collapsed, cached := h.hashFullNodeChildren(n)
		var size int
		hashed, size = h.fullnodeToHash(collapsed, force)
		if hn, ok := hashed.(hashNode); ok {
			cached.flags.hash = hn

			// record trie node connection: store child node hashes (jmlee)
			myHash := common.BytesToHash(hn)
			common.ChildHashesMutex.Lock()
			if nodeInfo, exist := common.TrieNodeInfosDirty[myHash]; !exist {
				nodeInfo.IsShortNode = false
				nodeInfo.Size = 32 + uint(size)
				for i := 0; i < 16; i++ {
					if child := cached.Children[i]; child != nil {
						childHashNode, _ := child.cache()
						var childHash common.Hash
						if childHashNode == nil {
							// child is hashNode (full node's child cannot be value node in Ethereum)
							childHash = common.BytesToHash([]byte(child.(hashNode)))
						} else {
							// child is full node or short node
							childHash = common.BytesToHash(childHashNode)
						}
						// fmt.Println("full node hash:", myHash.Hex(), " / child hash:", childHash.Hex())
						nodeInfo.ChildHashes = append(nodeInfo.ChildHashes, childHash)
						nodeInfo.Indices = append(nodeInfo.Indices, Indices[i])
					}
				}

				common.TrieNodeInfosDirty[myHash] = nodeInfo
			}
			common.ChildHashesMutex.Unlock()
		} else {
			cached.flags.hash = nil
		}
		return hashed, cached
	default:
		// Value and hash nodes don't have children so they're left as were
		return n, n
	}
}

// hashShortNodeChildren collapses the short node. The returned collapsed node
// holds a live reference to the Key, and must not be modified.
// The cached
func (h *hasher) hashShortNodeChildren(n *shortNode) (collapsed, cached *shortNode) {
	// Hash the short node's child, caching the newly hashed subtree
	collapsed, cached = n.copy(), n.copy()
	// Previously, we did copy this one. We don't seem to need to actually
	// do that, since we don't overwrite/reuse keys
	//cached.Key = common.CopyBytes(n.Key)
	collapsed.Key = hexToCompact(n.Key)
	// Unless the child is a valuenode or hashnode, hash it
	switch n.Val.(type) {
	case *fullNode, *shortNode:
		collapsed.Val, cached.Val = h.hash(n.Val, false)
	}
	return collapsed, cached
}

func (h *hasher) hashFullNodeChildren(n *fullNode) (collapsed *fullNode, cached *fullNode) {
	// Hash the full node's children, caching the newly hashed subtrees
	cached = n.copy()
	collapsed = n.copy()
	if h.parallel {
		var wg sync.WaitGroup
		wg.Add(16)
		for i := 0; i < 16; i++ {
			go func(i int) {
				hasher := newHasher(false)
				if child := n.Children[i]; child != nil {
					collapsed.Children[i], cached.Children[i] = hasher.hash(child, false)
				} else {
					collapsed.Children[i] = nilValueNode
				}
				returnHasherToPool(hasher)
				wg.Done()
			}(i)
		}
		wg.Wait()
	} else {
		for i := 0; i < 16; i++ {
			if child := n.Children[i]; child != nil {
				collapsed.Children[i], cached.Children[i] = h.hash(child, false)
			} else {
				collapsed.Children[i] = nilValueNode
			}
		}
	}
	return collapsed, cached
}

// shortnodeToHash creates a hashNode from a shortNode. The supplied shortnode
// should have hex-type Key, which will be converted (without modification)
// into compact form for RLP encoding.
// If the rlp data is smaller than 32 bytes, `nil` is returned.
func (h *hasher) shortnodeToHash(n *shortNode, force bool) (node, int) {
	h.tmp.Reset()
	if err := rlp.Encode(&h.tmp, n); err != nil {
		panic("encode error: " + err.Error())
	}

	if len(h.tmp) < 32 && !force {
		return n, len(h.tmp) // Nodes smaller than 32 bytes are stored inside their parent
	}
	return h.hashData(h.tmp), len(h.tmp)
}

// shortnodeToHash is used to creates a hashNode from a set of hashNodes, (which
// may contain nil values)
func (h *hasher) fullnodeToHash(n *fullNode, force bool) (node, int) {
	h.tmp.Reset()
	// Generate the RLP encoding of the node
	if err := n.EncodeRLP(&h.tmp); err != nil {
		panic("encode error: " + err.Error())
	}

	if len(h.tmp) < 32 && !force {
		return n, len(h.tmp) // Nodes smaller than 32 bytes are stored inside their parent
	}
	return h.hashData(h.tmp), len(h.tmp)
}

// hashData hashes the provided data
func (h *hasher) hashData(data []byte) hashNode {
	n := make(hashNode, 32)
	h.sha.Reset()
	h.sha.Write(data)
	h.sha.Read(n)
	return n
}

// proofHash is used to construct trie proofs, and returns the 'collapsed'
// node (for later RLP encoding) aswell as the hashed node -- unless the
// node is smaller than 32 bytes, in which case it will be returned as is.
// This method does not do anything on value- or hash-nodes.
func (h *hasher) proofHash(original node) (collapsed, hashed node, leng int) {
	switch n := original.(type) {
	case *shortNode:
		sn, _ := h.hashShortNodeChildren(n)
		hashed, leng := h.shortnodeToHash(sn, false)
		return sn, hashed, leng
	case *fullNode:
		fn, _ := h.hashFullNodeChildren(n)
		hashed, leng := h.fullnodeToHash(fn, false)
		return fn, hashed, leng
	default:
		// Value and hash nodes don't have children so they're left as were
		// leng is useless, so just set this as 0
		return n, n, 0
	}
}
