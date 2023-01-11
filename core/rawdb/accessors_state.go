// Copyright 2020 The go-ethereum Authors
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

package rawdb

import (
	"fmt"
	"os"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
)

// ReadPreimage retrieves a single preimage of the provided hash.
func ReadPreimage(db ethdb.KeyValueReader, hash common.Hash) []byte {
	data, _ := db.Get(preimageKey(hash))
	return data
}

// WritePreimages writes the provided set of preimages to the database.
func WritePreimages(db ethdb.KeyValueWriter, preimages map[common.Hash][]byte) {
	for hash, preimage := range preimages {
		if err := db.Put(preimageKey(hash), preimage); err != nil {
			log.Crit("Failed to store trie preimage", "err", err)
		}
	}
	preimageCounter.Inc(int64(len(preimages)))
	preimageHitCounter.Inc(int64(len(preimages)))
}

// ReadCode retrieves the contract code of the provided code hash.
func ReadCode(db ethdb.KeyValueReader, hash common.Hash) []byte {
	// Try with the prefixed code scheme first, if not then try with legacy
	// scheme.
	data := ReadCodeWithPrefix(db, hash)
	if len(data) != 0 {
		return data
	}
	data, _ = db.Get(hash[:])
	return data
}

// ReadCodeWithPrefix retrieves the contract code of the provided code hash.
// The main difference between this function and ReadCode is this function
// will only check the existence with latest scheme(with prefix).
func ReadCodeWithPrefix(db ethdb.KeyValueReader, hash common.Hash) []byte {
	data, _ := db.Get(codeKey(hash))
	return data
}

// HasCodeWithPrefix checks if the contract code corresponding to the
// provided code hash is present in the db. This function will only check
// presence using the prefix-scheme.
func HasCodeWithPrefix(db ethdb.KeyValueReader, hash common.Hash) bool {
	ok, _ := db.Has(codeKey(hash))
	return ok
}

// WriteCode writes the provided contract code database.
func WriteCode(db ethdb.KeyValueWriter, hash common.Hash, code []byte) {
	if err := db.Put(codeKey(hash), code); err != nil {
		log.Crit("Failed to store contract code", "err", err)
	}
}

// DeleteCode deletes the specified contract code from the database.
func DeleteCode(db ethdb.KeyValueWriter, hash common.Hash) {
	if err := db.Delete(codeKey(hash)); err != nil {
		log.Crit("Failed to delete contract code", "err", err)
	}
}

// ReadTrieNode retrieves the trie node of the provided hash.
func ReadTrieNode(db ethdb.KeyValueReader, hash common.Hash) []byte {
	data, _ := db.Get(hash.Bytes())
	return data
}

// HasTrieNode checks if the trie node with the provided hash is present in db.
func HasTrieNode(db ethdb.KeyValueReader, hash common.Hash) bool {
	ok, _ := db.Has(hash.Bytes())
	return ok
}

// flag
// WriteTrieNode writes the provided trie node database.
func WriteTrieNode(db ethdb.KeyValueWriter, hash common.Hash, node []byte) {

	// measuring db stat: # of trie nodes & db size for trie nodes (jmlee)
	// fmt.Println("WriteTrieNode() -> node hash:", hash.Hex())

	var alreadyFlushed bool
	if common.CollectNodeInfos {
		_, alreadyFlushed = common.TrieNodeInfos[hash]
	} else {
		_, alreadyFlushed = common.TrieNodeHashes[hash]
	}

	if !alreadyFlushed {
		// this new dirty node will be flushed
		nodeInfoDirty, existDirty := common.TrieNodeInfosDirty[hash]
		if !existDirty {
			fmt.Println("error !!! this should not happen")
			os.Exit(1)
		}

		nodeSize := uint64(nodeInfoDirty.Size) // size: 32 bytes hash + rlped node

		// update total db stat
		newNodeStat := &common.NewNodeStat
		totalNodeStat := &common.TotalNodeStat
		if common.FlushStorageTries {
			newNodeStat = &common.NewStorageNodeStat
			totalNodeStat = &common.TotalStorageNodeStat
		}
		if nodeInfoDirty.IsLeafNode {
			newNodeStat.LeafNodesNum++
			newNodeStat.LeafNodesSize += nodeSize

			totalNodeStat.LeafNodesNum++
			totalNodeStat.LeafNodesSize += nodeSize
		} else if nodeInfoDirty.IsShortNode {
			newNodeStat.ShortNodesNum++
			newNodeStat.ShortNodesSize += nodeSize

			totalNodeStat.ShortNodesNum++
			totalNodeStat.ShortNodesSize += nodeSize
		} else {
			newNodeStat.FullNodesNum++
			newNodeStat.FullNodesSize += nodeSize

			totalNodeStat.FullNodesNum++
			totalNodeStat.FullNodesSize += nodeSize
		}

		// measure inactive nodes independently
		if common.FlushInactiveTrie {
			newNodeStat := &common.NewInactiveNodeStat
			totalNodeStat := &common.TotalInactiveNodeStat

			if nodeInfoDirty.IsLeafNode {
				newNodeStat.LeafNodesNum++
				newNodeStat.LeafNodesSize += nodeSize
	
				totalNodeStat.LeafNodesNum++
				totalNodeStat.LeafNodesSize += nodeSize
			} else if nodeInfoDirty.IsShortNode {
				newNodeStat.ShortNodesNum++
				newNodeStat.ShortNodesSize += nodeSize
	
				totalNodeStat.ShortNodesNum++
				totalNodeStat.ShortNodesSize += nodeSize
			} else {
				newNodeStat.FullNodesNum++
				newNodeStat.FullNodesSize += nodeSize
	
				totalNodeStat.FullNodesNum++
				totalNodeStat.FullNodesSize += nodeSize
			}
		}

		// update current block's stat
		blockInfo, _ := common.Blocks[common.NextBlockNum]
		blockInfo.FlushedNodeHashes = append(blockInfo.FlushedNodeHashes, hash)
		common.Blocks[common.NextBlockNum] = blockInfo

		// check size is correct
		if nodeInfoDirty.Size != uint(32+len(node)) {
			fmt.Println("error! different size, this should not happen")
			fmt.Println(nodeInfoDirty.Size, uint(nodeSize))
			os.Exit(1)
		}

		// confirm dirty node
		if common.CollectNodeInfos {
			common.TrieNodeInfos[hash] = nodeInfoDirty
		} else {
			if common.CollectNodeHashes {
				common.TrieNodeHashes[hash] = struct{}{}
			}
		}
	}

	if err := db.Put(hash.Bytes(), node); err != nil {
		log.Crit("Failed to store trie node", "err", err)
		fmt.Println("WriteTrieNode() error:", err)
		fmt.Println("  node hash:", hash.Hex())
		fmt.Println("  rlped node:", node)
		os.Exit(1)
	}
}

// DeleteTrieNode deletes the specified trie node from the database.
func DeleteTrieNode(db ethdb.KeyValueWriter, hash common.Hash) {
	if err := db.Delete(hash.Bytes()); err != nil {
		log.Crit("Failed to delete trie node", "err", err)
	}
}
