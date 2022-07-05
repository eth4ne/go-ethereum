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

// Package state provides a caching layer atop the Ethereum state trie.
package state

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state/snapshot"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

type revision struct {
	id           int
	journalIndex int
}

var (
	// emptyRoot is the known root hash of an empty trie.
	emptyRoot = common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
)

// type proofList [][]byte // --> original code
type ProofList common.ProofList // (joonha)

func (n *ProofList) Put(key []byte, value []byte) error {
	*n = append(*n, value)
	return nil
}

func (n *ProofList) Delete(key []byte) error {
	panic("not supported")
}

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

// StateDB structs within the ethereum protocol are used to store anything
// within the merkle trie. StateDBs take care of caching and storing
// nested states. It's the general query interface to retrieve:
// * Contracts
// * Accounts
type StateDB struct {
	db           Database // actually this is leveldb (jmlee)
	prefetcher   *triePrefetcher
	originalRoot common.Hash // The pre-state root, before any changes were made
	trie         Trie
	hasher       crypto.KeccakState

	// original snapshot -> active snapshot (joonha)
	snaps         *snapshot.Tree
	snap          snapshot.Snapshot
	snapDestructs map[common.Hash]struct{}
	snapAccounts  map[common.Hash][]byte
	snapStorage   map[common.Hash]map[common.Hash][]byte

	// inactive storage snapshot (joonha)
	snaps_inactive			*snapshot.Tree
	snap_inactive			snapshot.Snapshot
	snapDestructs_inactive	map[common.Hash]struct{}
	snapAccounts_inactive 	map[common.Hash][]byte
	snapStorage_inactive	map[common.Hash]map[common.Hash][]byte

	// This map holds 'live' objects, which will get modified while processing a state transition.
	stateObjects        map[common.Address]*stateObject
	stateObjectsPending map[common.Address]struct{} // State objects finalized but not yet written to the trie
	stateObjectsDirty   map[common.Address]struct{} // State objects modified in the current execution

	// DB error.
	// State objects are used by the consensus core and VM which are
	// unable to deal with database-level errors. Any error that occurs
	// during a database read is memoized here and will eventually be returned
	// by StateDB.Commit.
	dbErr error

	// The refund counter, also used by state transitioning.
	refund uint64

	thash   common.Hash
	txIndex int
	logs    map[common.Hash][]*types.Log
	logSize uint

	preimages map[common.Hash][]byte

	// Per-transaction access list
	accessList *accessList

	// Journal of state modifications. This is the backbone of
	// Snapshot and RevertToSnapshot.
	journal        *journal
	validRevisions []revision
	nextRevisionId int

	// Measurements gathered during execution for debugging purposes
	AccountReads         time.Duration
	AccountHashes        time.Duration
	AccountUpdates       time.Duration
	AccountCommits       time.Duration
	StorageReads         time.Duration
	StorageHashes        time.Duration
	StorageUpdates       time.Duration
	StorageCommits       time.Duration
	SnapshotAccountReads time.Duration
	SnapshotStorageReads time.Duration
	SnapshotCommits      time.Duration

	AccountUpdated int
	StorageUpdated int
	AccountDeleted int
	StorageDeleted int

	// to implement compact MPT (jmlee)
	NextKey				int64 // key of the first 'will be inserted' account
	CheckpointKey		int64 // save initial NextKey value to determine whether move leaf nodes or not
	AddrToKeyDirty		map[common.Address]common.Hash // dirty cache for common.AddrToKey
	KeysToDeleteDirty	[]common.Hash // dirty cache for common.KeysToDelete

	// divide inactive from active part (joonha)
	AlreadyRestoredDirty 	map[common.Hash]common.Empty
	AddrToKeyDirty_inactive	map[common.Address][]common.Hash // dirty cache for common.AddrToKey_inactive
}

// New creates a new state from a given trie.
func New(root common.Hash, db Database, snaps *snapshot.Tree) (*StateDB, error) {
	tr, err := db.OpenTrie(root)
	if err != nil {
		return nil, err
	}

	// set NextKey as lastKey+1 (jmlee)
	lastKey := tr.GetLastKey()
	nextKey := new(big.Int)
	nextKey.Add(lastKey, big.NewInt(1))
	// fmt.Println("(1) next trie key to insert new leaf node:", nextKey.Int64())

	sdb := &StateDB{
		db:                  db,
		trie:                tr,
		originalRoot:        root,
		snaps:               snaps,
		snaps_inactive:		 snaps, // (joonha)
		stateObjects:        make(map[common.Address]*stateObject),
		stateObjectsPending: make(map[common.Address]struct{}),
		stateObjectsDirty:   make(map[common.Address]struct{}),
		logs:                make(map[common.Hash][]*types.Log),
		preimages:           make(map[common.Hash][]byte),
		journal:             newJournal(),
		accessList:          newAccessList(),
		hasher:              crypto.NewKeccakState(),
		// (jmlee)
		NextKey:		 	 nextKey.Int64(),
		CheckpointKey: 		 nextKey.Int64(),
		AddrToKeyDirty:	 	 make(map[common.Address]common.Hash),
		KeysToDeleteDirty:	 make([]common.Hash, 0),
		// (joonha)
		AlreadyRestoredDirty:make(map[common.Hash]common.Empty),
		AddrToKeyDirty_inactive:make(map[common.Address][]common.Hash),
	}
	if sdb.snaps != nil {
		if sdb.snap = sdb.snaps.Snapshot(root); sdb.snap != nil {
			sdb.snapDestructs = make(map[common.Hash]struct{})
			sdb.snapAccounts = make(map[common.Hash][]byte)
			sdb.snapStorage = make(map[common.Hash]map[common.Hash][]byte)
		}
	}
	// inactive storage snapshot (joonha)
	if sdb.snaps_inactive != nil {
		if sdb.snap_inactive == nil {
			sdb.snap_inactive = sdb.snaps_inactive.Snapshot(root)
		}
		sdb.snapDestructs_inactive = make(map[common.Hash]struct{})
		sdb.snapAccounts_inactive = make(map[common.Hash][]byte)
		sdb.snapStorage_inactive = make(map[common.Hash]map[common.Hash][]byte)
	}
	return sdb, nil
}

// Second New function to get snaps_inactive info from blockchain.go (joonha)
func New_inactiveSnapshot(root common.Hash, db Database, snaps *snapshot.Tree, snaps_inactive *snapshot.Tree) (*StateDB, error) {
	tr, err := db.OpenTrie(root)
	if err != nil {
		return nil, err
	}

	// set NextKey as lastKey+1 (jmlee)
	lastKey := tr.GetLastKey()
	nextKey := new(big.Int)
	nextKey.Add(lastKey, big.NewInt(1))
	// fmt.Println("\n(2) next trie key to insert new leaf node:", nextKey.Int64())
	sdb := &StateDB{
		db:                  db,
		trie:                tr,
		originalRoot:        root,
		snaps:               snaps,
		snaps_inactive:		 snaps_inactive, // (joonha)
		stateObjects:        make(map[common.Address]*stateObject),
		stateObjectsPending: make(map[common.Address]struct{}),
		stateObjectsDirty:   make(map[common.Address]struct{}),
		logs:                make(map[common.Hash][]*types.Log),
		preimages:           make(map[common.Hash][]byte),
		journal:             newJournal(),
		accessList:          newAccessList(),
		hasher:              crypto.NewKeccakState(),
		NextKey:		 	 nextKey.Int64(),
		CheckpointKey: 		 nextKey.Int64(),
		AddrToKeyDirty:	 	 make(map[common.Address]common.Hash),
		KeysToDeleteDirty:	 make([]common.Hash, 0),
		AlreadyRestoredDirty:make(map[common.Hash]common.Empty), // (joonha)
		AddrToKeyDirty_inactive:make(map[common.Address][]common.Hash), // (joonha)
	}
	if sdb.snaps != nil {
		if common.UsingActiveSnapshot {
			if sdb.snap = sdb.snaps.Snapshot(root); sdb.snap != nil {
				sdb.snapDestructs = make(map[common.Hash]struct{})
				sdb.snapAccounts = make(map[common.Hash][]byte)
				sdb.snapStorage = make(map[common.Hash]map[common.Hash][]byte)
			}
		}
	}
	// inactive storage snapshot (joonha)
	if sdb.snaps_inactive != nil {
		if common.UsingInactiveStorageSnapshot {
			sdb.snap_inactive = sdb.snaps_inactive.Snapshot(root) // watch out: insufficient funds err may occur
			sdb.snapDestructs_inactive = make(map[common.Hash]struct{})
			sdb.snapAccounts_inactive = make(map[common.Hash][]byte)
			sdb.snapStorage_inactive = make(map[common.Hash]map[common.Hash][]byte)
		}
	}
	return sdb, nil
}

// StartPrefetcher initializes a new trie prefetcher to pull in nodes from the
// state trie concurrently while the state is mutated so that when we reach the
// commit phase, most of the needed data is already hot.
func (s *StateDB) StartPrefetcher(namespace string) {
	if s.prefetcher != nil {
		s.prefetcher.close()
		s.prefetcher = nil
	}
	if s.snap != nil {
		// s.prefetcher = newTriePrefetcher(s.db, s.originalRoot, namespace) // --> original code

		// (joonha)
		if common.UsingActiveSnapshot {
			s.prefetcher = newTriePrefetcher(s.db, s.originalRoot, namespace)
		}
	}
}

// StopPrefetcher terminates a running prefetcher and reports any leftover stats
// from the gathered metrics.
func (s *StateDB) StopPrefetcher() {
	if s.prefetcher != nil {
		s.prefetcher.close()
		s.prefetcher = nil
	}
}

// setError remembers the first non-nil error it is called with.
func (s *StateDB) setError(err error) {
	if s.dbErr == nil {
		s.dbErr = err
	}
}

func (s *StateDB) Error() error {
	return s.dbErr
}

func (s *StateDB) AddLog(log *types.Log) {
	s.journal.append(addLogChange{txhash: s.thash})

	log.TxHash = s.thash
	log.TxIndex = uint(s.txIndex)
	log.Index = s.logSize
	s.logs[s.thash] = append(s.logs[s.thash], log)
	s.logSize++
}

func (s *StateDB) GetLogs(hash common.Hash, blockHash common.Hash) []*types.Log {
	logs := s.logs[hash]
	for _, l := range logs {
		l.BlockHash = blockHash
	}
	return logs
}

func (s *StateDB) Logs() []*types.Log {
	var logs []*types.Log
	for _, lgs := range s.logs {
		logs = append(logs, lgs...)
	}
	return logs
}

// AddPreimage records a SHA3 preimage seen by the VM.
func (s *StateDB) AddPreimage(hash common.Hash, preimage []byte) {
	if _, ok := s.preimages[hash]; !ok {
		s.journal.append(addPreimageChange{hash: hash})
		pi := make([]byte, len(preimage))
		copy(pi, preimage)
		s.preimages[hash] = pi
	}
}

// Preimages returns a list of SHA3 preimages that have been submitted.
func (s *StateDB) Preimages() map[common.Hash][]byte {
	return s.preimages
}

// AddRefund adds gas to the refund counter
func (s *StateDB) AddRefund(gas uint64) {
	s.journal.append(refundChange{prev: s.refund})
	s.refund += gas
}

// SubRefund removes gas from the refund counter.
// This method will panic if the refund counter goes below zero
func (s *StateDB) SubRefund(gas uint64) {
	s.journal.append(refundChange{prev: s.refund})
	if gas > s.refund {
		panic(fmt.Sprintf("Refund counter below zero (gas: %d > refund: %d)", gas, s.refund))
	}
	s.refund -= gas
}

// Exist reports whether the given account address exists in the state.
// Notably this also returns true for suicided accounts.
func (s *StateDB) Exist(addr common.Address) bool {
	return s.getStateObject(addr) != nil
}

// Empty returns whether the state object is either non-existent
// or empty according to the EIP161 specification (balance = nonce = code = 0)
func (s *StateDB) Empty(addr common.Address) bool {
	so := s.getStateObject(addr)
	return so == nil || so.empty()
}

// GetBalance retrieves the balance from the given address or 0 if object not found
func (s *StateDB) GetBalance(addr common.Address) *big.Int {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.Balance()
	}
	return common.Big0
}

func (s *StateDB) GetNonce(addr common.Address) uint64 {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.Nonce()
	}

	return 0
}

// TxIndex returns the current transaction index set by Prepare.
func (s *StateDB) TxIndex() int {
	return s.txIndex
}

func (s *StateDB) GetCode(addr common.Address) []byte {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.Code(s.db)
	}
	return nil
}

func (s *StateDB) GetCodeSize(addr common.Address) int {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.CodeSize(s.db)
	}
	return 0
}

func (s *StateDB) GetCodeHash(addr common.Address) common.Hash {
	stateObject := s.getStateObject(addr)
	if stateObject == nil {
		return common.Hash{}
	}
	return common.BytesToHash(stateObject.CodeHash())
}

// GetState retrieves a value from the given account's storage trie.
func (s *StateDB) GetState(addr common.Address, hash common.Hash) common.Hash {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.GetState(s.db, hash)
	}
	return common.Hash{}
}

// // GetProof returns the Merkle proof for a given account. // --> original code
// func (s *StateDB) GetProof(addr common.Address) ([][]byte, error) {
// 	return s.GetProofByHash(crypto.Keccak256Hash(addr.Bytes()))
// }

// // Just one recent node (joonha)
// // GetProof returns the Merkle proof for a given account.
// func (s *StateDB) GetProof(addr common.Address) ([][]byte, error) {
// 	if len(common.AddrToKey_inactive[addr]) <= 0 {
// 		return nil, errors.New("No Account to Restore (ethane) (1)") //////////// 여기에 걸리네
// 	}
// 	lastIndex := len(common.AddrToKey_inactive[addr]) - 1
// 	// fmt.Println("lastIndex is ", lastIndex)
// 	// fmt.Println("addrToKey_inactive is ", common.AddrToKey_inactive[addr])
// 	if common.AddrToKey_inactive[addr][lastIndex] == common.ToBeDeletedKey {
// 		return nil, errors.New("No Account to Restore (ethane) (2)")
// 	}
// 	_, ok := common.AddrToKey_inactive[addr];
// 	if !ok { // empty map
// 		// fmt.Println("AddrToKey_inactive mapped value does not exist")
// 	}
// 	key := common.AddrToKey_inactive[addr][lastIndex]
// 	return s.GetProofByHash(key)
// }

// 2022-06-09 (joonha)
// GetProof returns the Merkle proofs for *ALL KEYS* of the given account.
func (s *StateDB) GetProof(addr common.Address) ([][]byte, error) {
	
	// fmt.Println("\ncommon.RestoreMode: ", common.RestoreMode)
	// fmt.Println("common.RestoreAmount: ", common.RestoreAmount,"\n")
	// common.RestoreMode:
	// 0 All
	// 1 RECENT
	// 2 OLDEST
	// 3 OPTIMIZED (w/ common.RestoreAmount)

	
	if len(common.AddrToKey_inactive[addr]) <= 0 {
		return nil, errors.New("No Account to Restore (ethane) (1)")
	}

	/***********************************/
	// GET PROOFS
	/***********************************/
	// optimize opportunity: store balance of inactive accounts with AddrToKey_inactive from the first (TODO)
	var mps [][]byte

	if common.RestoreMode == 0 { // ALL
		for i := 0; i < len(common.AddrToKey_inactive[addr]); i++ {
			key := common.AddrToKey_inactive[addr][i]
			if key != common.ToBeDeletedKey {
				mp, _ := s.GetProofByHash(key)
				mps = append(mps, mp...)
			}
		}
	} else if common.RestoreMode == 1 { // RECENT
		for i := len(common.AddrToKey_inactive[addr])-1; i >= 0; i-- {
			key := common.AddrToKey_inactive[addr][i]
			if key != common.ToBeDeletedKey { // found valid inactive account
				mp, _ := s.GetProofByHash(key)
				mps = append(mps, mp...)
				break;
			}
		}
	} else if common.RestoreMode == 2 { // OLDEST
		for i := 0; i < len(common.AddrToKey_inactive[addr]); i++ {
			key := common.AddrToKey_inactive[addr][i]
			if key != common.ToBeDeletedKey { // found valid inactive account
				mp, _ := s.GetProofByHash(key)
				mps = append(mps, mp...)
				break;
			}
		}
	} else if common.RestoreMode == 3 { // OPTIMIZED
		type myDataType struct {
			index int
			balance *big.Int
		}
		mySlice := make([]myDataType, 0)

		for i := 0; i < len(common.AddrToKey_inactive[addr]); i++ {
			key := common.AddrToKey_inactive[addr][i]
			if key != common.ToBeDeletedKey { // found valid inactive account
				// balance
				enc, _ := s.trie.TryGet_SetKey(key[:])
				// var data *types.StateAccount
				data := new(types.StateAccount) //...?
				rlp.DecodeBytes(enc, data)
				// fmt.Println("Balance of ", i, " is ", data.Balance)
				var myData myDataType
				myData.index = i
				myData.balance = data.Balance
				mySlice = append(mySlice, myData)
			}
		}
		// sort by balance (ascending order)
		sort.Slice(mySlice, func(i, j int) bool {
			return (mySlice[j].balance.Cmp(mySlice[i].balance) == 1)
		})
		// [2, 3, 3] 이고 request 5일 땐, 3 + 3을 restore 하도록 구현되어 있음.
		// MP 크기가 작다면 2 + 3 을 restore 하는 것이 더 최적일 수 있으나, 이는 당장은 고려하지 않음. (과한 최적화 같아서.)
		sum := big.NewInt(0)
		for i := len(mySlice)-1; i >= 0; i-- {
			// fmt.Println("Restoring ", mySlice[i].index, "th inactive account (indiv balance:", mySlice[i].balance, ")")
			sum.Add(sum, mySlice[i].balance)
			mp, _ := s.GetProofByHash(common.AddrToKey_inactive[addr][mySlice[i].index])
			mps = append(mps, mp...)
			if sum.Cmp(common.RestoreAmount) >= 0 {
				break;
			}
		}
		// fmt.Println("\nRequested Balance: ", common.RestoreAmount)
		// fmt.Println("Total Restored Balance: ", sum)
		if sum.Cmp(common.RestoreAmount) < 0 {
			log.Error("The requested balance is bigger than the total inactive balances - anyway, we will restore the total")
		}
	}
	

	/***********************************/
	// REMOVE REDUNDANT NODES
	/***********************************/
	// fmt.Println("\n\nmps ===>")
	// fmt.Println(mps, "\n===> end of mps\n\n")

	var mps_no_redundancy [][]byte
	m := make(map[string]int)
	// dummy := []byte{'@'}
	// fmt.Println("\n\ndummy ===>")
	// fmt.Println(dummy, "\n===> end of dummy\n\n")

	for idx, node := range mps {
    	if _, ok := m[string(node[:])]; !ok {
			// first appearance
            m[string(node[:])] = idx
            mps_no_redundancy = append(mps_no_redundancy, node)
    	} else if ok {
			// redundant
			dummy := []byte{'@'}
			dummy = append(dummy, byte(m[string(node[:])])) // dummy: @ + index of the ref node
			// 아마 byte -> int 로 바꿔야 원래 index를 retrieve 할 수 있을 것임.
			mps_no_redundancy = append(mps_no_redundancy, dummy)
		}
	}
	// fmt.Println("\n\nmps_no_redundancy ===>")
	// fmt.Println(mps_no_redundancy, "\n===> end of mps_no_redundancy\n\n")

	// return nil, errors.New("Let's stop here and watch the MPs")

	return mps_no_redundancy, nil
}

// GetProofByHash returns the Merkle proof for a given account.
func (s *StateDB) GetProofByHash(addrHash common.Hash) ([][]byte, error) {
	var proof ProofList
	err := s.trie.Prove(addrHash[:], 0, &proof)
	return proof, err
}

// GetStorageProof returns the Merkle proof for given storage slot.
func (s *StateDB) GetStorageProof(a common.Address, key common.Hash) ([][]byte, error) {
	var proof ProofList
	trie := s.StorageTrie(a)
	if trie == nil {
		return proof, errors.New("storage trie for requested address does not exist")
	}
	err := trie.Prove(crypto.Keccak256(key.Bytes()), 0, &proof)
	return proof, err
}

// GetCommittedState retrieves a value from the given account's committed storage trie.
func (s *StateDB) GetCommittedState(addr common.Address, hash common.Hash) common.Hash {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.GetCommittedState(s.db, hash)
	}
	return common.Hash{}
}

// Database retrieves the low level database supporting the lower level trie ops.
func (s *StateDB) Database() Database {
	return s.db
}

// StorageTrie returns the storage trie of an account.
// The return value is a copy and is nil for non-existent accounts.
func (s *StateDB) StorageTrie(addr common.Address) Trie {
	stateObject := s.getStateObject(addr)
	if stateObject == nil {
		return nil
	}
	cpy := stateObject.deepCopy(s)
	cpy.updateTrie(s.db)
	return cpy.getTrie(s.db)
}

func (s *StateDB) HasSuicided(addr common.Address) bool {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.suicided
	}
	return false
}

/*
 * SETTERS
 */

// AddBalance adds amount to the account associated with addr.
func (s *StateDB) AddBalance(addr common.Address, amount *big.Int) {
	stateObject := s.GetOrNewStateObject(addr)
	if stateObject != nil {
		stateObject.AddBalance(amount)
	}
}

// SubBalance subtracts amount from the account associated with addr.
func (s *StateDB) SubBalance(addr common.Address, amount *big.Int) {
	stateObject := s.GetOrNewStateObject(addr)
	if stateObject != nil {
		stateObject.SubBalance(amount)
	}
}

func (s *StateDB) SetBalance(addr common.Address, amount *big.Int) {
	stateObject := s.GetOrNewStateObject(addr)
	if stateObject != nil {
		stateObject.SetBalance(amount)
	}
}

func (s *StateDB) SetNonce(addr common.Address, nonce uint64) {
	stateObject := s.GetOrNewStateObject(addr)
	if stateObject != nil {
		stateObject.SetNonce(nonce)
	}
}

func (s *StateDB) SetCode(addr common.Address, code []byte) {
	stateObject := s.GetOrNewStateObject(addr)
	if stateObject != nil {
		stateObject.SetCode(crypto.Keccak256Hash(code), code)
	}
}

// hashing code is not needed during restoring (joonha)
func (s *StateDB) SetCode_Restore(addr common.Address, code []byte) {
	stateObject := s.GetOrNewStateObject(addr)
	if stateObject != nil {
		stateObject.SetCode(common.BytesToHash(code), code)
	}
}

func (s *StateDB) SetState(addr common.Address, key, value common.Hash) {
	stateObject := s.GetOrNewStateObject(addr)
	if stateObject != nil {
		stateObject.SetState(s.db, key, value)
	}
}

// SetState_Restore set storage slot when restoring CA (joonha)
func (s *StateDB) SetState_Restore(addr common.Address, key, value common.Hash) {
	// get key
	addrKey, doExist := s.AddrToKeyDirty[addr]
	if !doExist {
		addrKey = common.AddrToKey[addr]
	}
	addrKey_bigint := new(big.Int)
	addrKey_bigint.SetString(addrKey.Hex()[2:], 16)

	// get renewed account
	var stateObject *stateObject
	if addrKey_bigint.Int64() >= common.InactiveBoundaryKey { // restore by merge
		// fmt.Println("RESTORING BY MERGE\n")
	} else { // restore by create
		// fmt.Println("RESTORING BY CREATE\n")
	}
	stateObject = s.getStateObject(addr) 

	if stateObject != nil {
		stateObject.SetState_Restore(s.db, key, value)
	}
}

// SetStorage replaces the entire storage for the specified account with given
// storage. This function should only be used for debugging.
func (s *StateDB) SetStorage(addr common.Address, storage map[common.Hash]common.Hash) {
	stateObject := s.GetOrNewStateObject(addr)
	if stateObject != nil {
		stateObject.SetStorage(storage)
	}
}

// Suicide marks the given account as suicided.
// This clears the account balance.
//
// The account's state object is still available until the state is committed,
// getStateObject will return a non-nil account after Suicide.
func (s *StateDB) Suicide(addr common.Address) bool {
	stateObject := s.getStateObject(addr)
	if stateObject == nil {
		return false
	}
	s.journal.append(suicideChange{
		account:     &addr,
		prev:        stateObject.suicided,
		prevbalance: new(big.Int).Set(stateObject.Balance()),
	})
	stateObject.markSuicided()
	stateObject.data.Balance = new(big.Int)

	return true
}

//
// Setting, updating & deleting state object methods.
//

// updateStateObject writes the given object to the trie.
func (s *StateDB) updateStateObject(obj *stateObject) {
	// Track the amount of time wasted on updating the account from the trie
	if metrics.EnabledExpensive {
		defer func(start time.Time) { s.AccountUpdates += time.Since(start) }(time.Now())
	}
	// Encode the account and update the account trie
	addr := obj.Address()


	/**************************/
	// ORIGINAL CODE
	/**************************/
	// if err := s.trie.TryUpdateAccount(addr[:], &obj.data); err != nil {
	// 	s.setError(fmt.Errorf("updateStateObject (%x) error: %v", addr[:], err))
	// }
	

	/**************************/
	// v1.10.3
	/**************************/
	// to delete prev snapshot (joonha)
	prevAddrHash := obj.addrHash

	data, err := rlp.EncodeToBytes(obj)
	if err != nil {
		panic(fmt.Errorf("can't encode object at %x: %v", addr[:], err))
	}

	// codes for compact trie (jmlee)
	// get addrKey of this address 
	addrKey, doExist := s.AddrToKeyDirty[addr]
	if !doExist {
		addrKey = common.AddrToKey[addr]
	}
	addrKey_bigint := new(big.Int)
	addrKey_bigint.SetString(addrKey.Hex()[2:], 16)
	// fmt.Println("addrKey_bigint:", addrKey_bigint.Int64(), "/ CheckpointKey:", s.CheckpointKey)

	// fmt.Println("\n[Account]\nNonce: ", obj.data.Nonce)
	// fmt.Println("Balance: ", obj.data.Balance)
	// fmt.Println("CodeHash: ", obj.data.CodeHash)
	// fmt.Println("Root: ", obj.data.Root)
	// fmt.Println("Addr: ", obj.data.Addr)
	// fmt.Println("")

	if addrKey_bigint.Int64() >= s.CheckpointKey {
		// fmt.Println("\n\nupdateStateObject ----------> first case\n\n")
		/******************************************************/
		// UPDATED AT THIS EPOCH
		/******************************************************/
		// this address is newly created address OR already moved address. so just update
		// fmt.Println("insert -> key:", addrKey.Hex(), "/ addr:", addr.Hex())
		if err = s.trie.TryUpdate_SetKey(addrKey[:], data); err != nil {
			s.setError(fmt.Errorf("updateStateObject (%x) error: %v", addr[:], err))
		}

	} else if addrKey_bigint.Int64() >= common.InactiveBoundaryKey {
		// fmt.Println("\n\nupdateStateObject ----------> second case\n\n")
		/******************************************************/
		// ACTIVE AT PREVIOUS EPOCH BUT NOT INACTIVATED YET
		/******************************************************/
		// This address is already in the trie, so move the previous leaf node to the right side (delete & insert)
		// Do not delete this now, just append s.KeysToDeleteDirty to delete them at once later (periodical delete)
		// fmt.Println("append this to KeysToDeleteDirty to delete later -> key:", addrKey.Hex(), "/ addr:", addr.Hex())
		s.KeysToDeleteDirty = append(s.KeysToDeleteDirty, addrKey)

		// insert new leaf node at right side
		newAddrHash := common.HexToHash(strconv.FormatInt(s.NextKey, 16))
		s.AddrToKeyDirty[addr] = newAddrHash
		// fmt.Println("insert -> key:", newAddrHash.Hex(), "/ addr:", addr.Hex())
		if err = s.trie.TryUpdate_SetKey(newAddrHash[:], data); err != nil {
			s.setError(fmt.Errorf("updateStateObject (%x) error: %v", addr[:], err))
		}
		// fmt.Println("move leaf node to right -> addr:", addr.Hex(), "/ keyHash:", newAddrHash)
		obj.addrHash = newAddrHash
		s.NextKey += 1

	} else { // < InactiveBoundaryKey : creating a crumb or restoring by creating (joonha) 
		// fmt.Println("\n\nupdateStateObject ----------> third case\n\n")
		/***********************************************************/
		// HAD BEEN INACTIVATED SO THIS SEEMS NEW TO ACTIVE TRIE
		/***********************************************************/
		// Adding a new account where inactive account might already exist(there CAN be an inactive account).
		// If then, act like this account is a new account. 
		// Also updating AddrToKey is needed.
		// No deletion is needed.

		// deleting prev(inactive)Account snapshot occurs in RebuildStorageTrieFromSnapshot
		s.KeysToDeleteDirty = append(s.KeysToDeleteDirty, addrKey)

		// insert new leaf node at right side
		newAddrHash := common.HexToHash(strconv.FormatInt(s.NextKey, 16))
		s.AddrToKeyDirty[addr] = newAddrHash
		// fmt.Println("(updateStateObject)insert -> key:", newAddrHash.Hex(), "/ addr:", addr.Hex())

		if err = s.trie.TryUpdate_SetKey(newAddrHash[:], data); err != nil {
			s.setError(fmt.Errorf("updateStateObject (%x) error: %v", addr[:], err))
		}
		// fmt.Println("move leaf node to right -> addr:", addr.Hex(), "/ keyHash:", newAddrHash)
		obj.addrHash = newAddrHash
		s.NextKey += 1
	}

	// If state snapshotting is active, cache the data til commit. Note, this
	// update mechanism is not symmetric to the deletion, because whereas it is
	// enough to track account updates at commit time, deletions need tracking
	// at transaction boundary level to ensure we capture state clearing.
	if s.snap != nil {
		// original code
		// s.snapAccounts[obj.addrHash] = snapshot.SlimAccountRLP(obj.data.Nonce, obj.data.Balance, obj.data.Root, obj.data.CodeHash)

		if common.UsingActiveSnapshot {
			// fmt.Println("addr:", obj.Address().Hex(), "update snapAccounts -> addrKey:", obj.addrHash.Hex())
			// fmt.Println("(USO) Addr: ", obj.data.Addr)
			// fmt.Println("(USO) Nonce: ", obj.data.Nonce)
			// fmt.Println("(USO) Balance: ", obj.data.Balance)
			// fmt.Println("(USO) Root: ", obj.data.Root)
			// fmt.Println("(USO) CodeHash: ", obj.data.CodeHash)

			s.snapAccounts[obj.addrHash] = snapshot.SlimAccountRLP(obj.data.Nonce, obj.data.Balance, obj.data.Root, obj.data.CodeHash, addr)
			// fmt.Println("\n>>> snapshot.SlimAccountRLP( ): ", s.snapAccounts[obj.addrHash], "\n")

			// snapshot update (joonha)
			if obj.addrHash != prevAddrHash { // key has been changed
				temp := make(map[common.Hash][]byte)
				for kk, vv := range s.snapStorage[prevAddrHash] {
					temp[kk] = vv
				}
				s.snapStorage[obj.addrHash] = temp

				s.snapDestructs[prevAddrHash] = struct{}{} // add this to snapshot's delete list
				delete(s.snapAccounts, prevAddrHash) // delete this from snapshot's update list
				delete(s.snapStorage, prevAddrHash) // delete this from snapshot's update list
			}
		}
	}
}

// deleteStateObject removes the given object from the state trie.
func (s *StateDB) deleteStateObject(obj *stateObject) {
	// Track the amount of time wasted on deleting the account from the trie
	if metrics.EnabledExpensive {
		defer func(start time.Time) { s.AccountUpdates += time.Since(start) }(time.Now())
	}
	// Delete the account from the trie
	addr := obj.Address()
	if err := s.trie.TryDelete(addr[:]); err != nil {
		s.setError(fmt.Errorf("deleteStateObject (%x) error: %v", addr[:], err))
	}
}

// getStateObject retrieves a state object given by the address, returning nil if
// the object is not found or was deleted in this execution context. If you need
// to differentiate between non-existent/just-deleted, use getDeletedStateObject.
func (s *StateDB) getStateObject(addr common.Address) *stateObject {
	// if obj := s.getDeletedStateObject(addr); obj != nil && !obj.deleted { // --> original code
	if obj := s.getDeletedStateObject(addr, 0); obj != nil && !obj.deleted { // get obj from the active trie (joonha)
		return obj
	}
	return nil
}

// getStateObject_FromInactiveTrie retrieves a state object from inactive trie given by the address (joonha)
func (s *StateDB) getStateObject_FromInactiveTrie(addr common.Address) *stateObject {
	if obj := s.getDeletedStateObject(addr, 1); obj != nil && !obj.deleted { // get obj from the active trie (joonha)
		return obj
	}
	return nil
}

// getDeletedStateObject is similar to getStateObject, but instead of returning
// nil for a deleted state object, it returns the actual object with the deleted
// flag set. This is needed by the state journal to revert to the correct s-
// destructed object instead of wiping all knowledge about the state object.
// func (s *StateDB) getDeletedStateObject(addr common.Address) *stateObject { // --> original code
func (s *StateDB) getDeletedStateObject(addr common.Address, restoring int64) *stateObject { // restoring option (whether to get account from active or inactive trie) has been added (joonha)
	// TODO(joonha) restoring 파라미터 이름을 active/inactive 로 바꾸는 것이 좋아보임.
	
	// Prefer live objects if any is available
	if obj := s.stateObjects[addr]; obj != nil {
		return obj
	}

	
	/******************************/
	// v1.10.3
	/******************************/
	// If no live objects are available, attempt to use snapshots
	// here is where a snapshot works (jmlee)
	var (
		// data *Account
		data *types.StateAccount
		err  error
	)


	/****************************************************/
	// SNAPSHOT
	/****************************************************/
	if s.snap != nil {
		// start := time.Now()
		// acc, err := s.snap.Account(crypto.HashData(s.hasher, addr.Bytes()))

		// fmt.Println("\n<<<<<<<<<<<<<<<<<<< getDeletedStateObject >>>>>>>>>>>>>>>>>>>> snapshot ON")

		/***********************************/
		// PRINT TO DEBUG
		/***********************************/
		// // print some of snapshot info for debugging (jmlee)
		// maxIndexToPrint := int64(15)
		// if s.snap != nil {
		// 	for i := int64(0); i < maxIndexToPrint; i++ {
		// 		key := common.HexToHash(strconv.FormatInt(i, 16))
		// 		if acc, err := s.snap.Account(key); err == nil {
		// 			if acc == nil {
		// 				fmt.Println("snapshot[",i,"]: nil")
		// 			} else {
		// 				fmt.Println("snapshot[",i,"]: exist ->", acc)
		// 			}
		// 		}
		// 	}
		// 	if common.UsingInactiveStorageSnapshot && s.snap_inactive != nil {
		// 		for i := int64(0); i < maxIndexToPrint; i++ {
		// 			key := common.HexToHash(strconv.FormatInt(i, 16))
		// 			if acc, err := s.snap_inactive.Account(key); err == nil {
		// 				if acc == nil {
		// 					fmt.Println("snapshot_inactive[",i,"]: nil")
		// 				} else {
		// 					fmt.Println("snapshot_inactive[",i,"]: exist ->", acc)
		// 				}
		// 			}
		// 		}
		// 	}
		// }

		if metrics.EnabledExpensive {
			// s.SnapshotAccountReads += time.Since(start)
			// defer func(start time.Time) { s.SnapshotAccountReads += time.Since(start) }(time.Now()) // v1.10.3 (joonha)
		}
		var acc *snapshot.Account

		/************************************/
		// GET KEY
		/************************************/
		// change key to make compactTrie (jmlee)
		key, doExist := common.AddrToKey[addr]
		// fmt.Println("original key is ", key) //
		// fmt.Println("restoring: ", restoring) //
		if !doExist {
			key = common.NoExistKey
		}

		// get key from inactive trie (during restoring) (joonha) --> is this really the case? TODO (joonha)
		if restoring == 1 {
			lastIndex := len(common.AddrToKey_inactive[addr]) - 1
			if lastIndex < 0 { // error handling
				return nil
			}
			key = common.AddrToKey_inactive[addr][lastIndex]
		}

		// if cannot find the account, return nil (joonha)
		if restoring == 0 && common.HashToInt64(key) < common.InactiveBoundaryKey {
			// fmt.Println("cannot find the account in the active trie (it was inactivated)")
			return nil
		} else if restoring == 0 && key == common.NoExistKey {
			// fmt.Println("can't find the account")
			return nil
		} else if restoring == 0 && common.HashToInt64(key) >= common.InactiveBoundaryKey {
			// fmt.Println("can find the account in the active trie")
		} 
		// fmt.Println("Try to find account at the snapshot -> addr:", addr.Hex(), "/ key:", key.Hex())

		/***********************************/
		// GET KEY'S OBJECT
		/***********************************/
		if restoring == 0 {
			// fmt.Println("getDeletedStateObject >> restoring = 0 >> get account from snap ")
			if acc, err = s.snap.Account(key); err == nil {
				// if acc, err = s.snap.Account(crypto.HashData(s.hasher, addr.Bytes())); err == nil { // -> original code
				if acc == nil {
					// fmt.Println("  cannot find the address at the snapshot")
					return nil
				}
				// data = &Account{
				data = &types.StateAccount { // v1.10.16
					Nonce:    acc.Nonce,
					Balance:  acc.Balance,
					CodeHash: acc.CodeHash,
					Root:     common.BytesToHash(acc.Root),
					Addr:	  acc.Addr, // core/state/snapshot/account.go (joonha)
				}
				if len(data.CodeHash) == 0 {
					data.CodeHash = emptyCodeHash
				}
				if data.Root == (common.Hash{}) {
					data.Root = emptyRoot
				}
				// fmt.Println("\n(GDSO) Nonce: ", data.Nonce)
				// fmt.Println("(GDSO) Balance: ", data.Balance)
				// fmt.Println("(GDSO) CodeHash: ", data.CodeHash)
				// fmt.Println("(GDSO) Root: ", data.Root)
				// fmt.Println("(GDSO) Addr: ", data.Addr)
			}
		} else if restoring == 1 { // during restoring, should get the account from inactive trie database not using the snapshot
			// fmt.Println("getDeletedStateObject >> restoring = 1 >> get account from trie ")
			enc, err := s.trie.TryGet_SetKey(key[:])
			if err != nil {
				s.setError(fmt.Errorf("getDeleteStateObject (%x) error: %v", addr.Bytes(), err))
				return nil
			}
			if len(enc) == 0 {
				// fmt.Println("cannot find the address outside of the snapshot (1)")
				return nil
			}
			// data = new(Account)
			data = new(types.StateAccount) // v1.10.16
			if err := rlp.DecodeBytes(enc, data); err != nil {
				log.Error("Failed to decode state object", "addr", addr, "err", err)
				return nil
			}
		}
	}

	

	/****************************************************/
	// NO SNAPSHOT
	/****************************************************/
	// If snapshot unavailable or reading from it failed, load from the database

	if s.snap == nil || err != nil {
		// fmt.Println("\n<<<<<<<<<<<<<<<<<<< getDeletedStateObject >>>>>>>>>>>>>>>>>>>> snapshot OFF")
		// fmt.Println("Try to find account without snapshot -> addr:", addr.Hex())
		if metrics.EnabledExpensive {
			// defer func(start time.Time) { s.AccountReads += time.Since(start) }(time.Now()) // v1.10.3 (joonha)
		}

		/************************************/
		// GET KEY
		/************************************/
		// key := common.AddrToKey[addr]
		key, doExist := common.AddrToKey[addr]
		// fmt.Println("original key is ", key)
		// fmt.Println("restoring: ", restoring)
		if !doExist {
			key = common.NoExistKey
		}
		// during restoration, get an obj from the inactiveTrie
		// if lightNode, key should be nil or something that cannot be retrieved from AddrToKey_inactive
		if restoring == 1 {
			lastIndex := len(common.AddrToKey_inactive[addr]) - 1
			if lastIndex < 0 { // error handling
				return nil
			}
			key = common.AddrToKey_inactive[addr][lastIndex]
			// fmt.Println("inactive key is ", key)
		}
		// if the account has already been inactivated, return nil (crumb account) (joonha)
		if restoring == 0 && common.HashToInt64(key) < common.InactiveBoundaryKey {
			// fmt.Println("key is ", key)
			// fmt.Println("can't get the account because it is inactivated")
			return nil
		} else if restoring == 0 && key == common.NoExistKey {
			// fmt.Println("key is ", key)
			// fmt.Println("there is no account (retrieved from AddrToKey)")
			return nil
		} else if restoring == 0 && common.HashToInt64(key) >= common.InactiveBoundaryKey {
			// fmt.Println("key is ", key)
			// fmt.Println("the account is in the active trie")
		}

		/***********************************/
		// GET KEY'S OBJECT
		/***********************************/
		enc, err := s.trie.TryGet_SetKey(key[:])
		// enc, err := s.trie.TryGet(addr.Bytes()) // get account from state trie // --> original code
		if err != nil {
			s.setError(fmt.Errorf("getDeleteStateObject (%x) error: %v", addr.Bytes(), err))
			return nil
		}
		if len(enc) == 0 {
			// fmt.Println("cannot find the address outside of the snapshot (2)")
			return nil
		}
		// data = new(Account) // v1.10.3
		data = new(types.StateAccount) // v1.10.16

		if err := rlp.DecodeBytes(enc, data); err != nil {
			log.Error("Failed to decode state object", "addr", addr, "err", err)
			return nil
		}
	}

	// Insert into the live set
	obj := newObject(s, addr, *data)
	s.setStateObject(obj)
	return obj
}

func (s *StateDB) setStateObject(object *stateObject) {
	s.stateObjects[object.Address()] = object
}

// GetOrNewStateObject retrieves a state object or create a new state object if nil.
func (s *StateDB) GetOrNewStateObject(addr common.Address) *stateObject {
	stateObject := s.getStateObject(addr)
	if stateObject == nil {
		// stateObject, _ = s.createObject(addr) // --> original code
		stateObject, _ = s.createObject(addr, nil) // (joonha)
	}
	return stateObject
}

// createObject creates a new state object. If there is an existing account with
// the given address, it is overwritten and returned as the second return value.
// func (s *StateDB) createObject(addr common.Address) (newobj, prev *stateObject) { // --> original code
func (s *StateDB) createObject(addr common.Address, blockNum *big.Int) (newobj, prev *stateObject) { // (joonha)

	// insert to map to make compactTrie (jmlee)
	key, doExist := common.AddrToKey[addr]
	if !doExist && addr != common.ZeroAddress { // creating whole-new account
		newAddrKey := common.HexToHash(strconv.FormatInt(s.NextKey, 16))
		// fmt.Println("make new account -> addr:", addr.Hex(), "/ keyHash:", newAddrKey)
		s.AddrToKeyDirty[addr] = newAddrKey
		s.NextKey += 1
	} else if doExist && common.HashToInt64(key) < s.CheckpointKey { // creating crumb account (joonha)
		newAddrKey := common.HexToHash(strconv.FormatInt(s.NextKey, 16))
		s.AddrToKeyDirty[addr] = newAddrKey
		s.NextKey += 1
	}

	// (joonha)
	// creating a crumb account, don't get the prev account
	if doExist && common.HashToInt64(key) < s.CheckpointKey {
		prev = nil
	} else { // get the object from the active trie
		prev = s.getDeletedStateObject(addr, 0) // Note, prev might have been deleted, we need that!
	}

	var prevdestruct bool
	if s.snap != nil && prev != nil {
		// _, prevdestruct = s.snapDestructs[prev.addrHash]
		// if !prevdestruct {
		// 	s.snapDestructs[prev.addrHash] = struct{}{}
		// }

		// check the snapshot option (joonha)
		if common.UsingActiveSnapshot {
			_, prevdestruct = s.snapDestructs[prev.addrHash]
			if !prevdestruct {
				s.snapDestructs[prev.addrHash] = struct{}{}
			}
		}
	}
	newobj = newObject(s, addr, types.StateAccount{})
	if prev == nil {
		s.journal.append(createObjectChange{account: &addr})
	} else {
		s.journal.append(resetObjectChange{prev: prev, prevdestruct: prevdestruct})
	}

	// if creating a crumb account, set nonce to blockNum * 64 (joonha)
	if doExist && common.HashToInt64(key) < s.CheckpointKey {
		if blockNum != nil {
			newNonce := big.NewInt(0)
			newNonce.Mul(blockNum, big.NewInt(64))
			newobj.setNonce(newNonce.Uint64())
		}
	}

	s.setStateObject(newobj)
	if prev != nil && !prev.deleted {
		return newobj, prev
	}
	return newobj, nil
}

// createObject_restoring creates an object restored from the inactive trie (joonha)
func (s *StateDB) createObject_restoring(addr common.Address) (newobj, prev *stateObject) {

	// insert to map to make compactTrie (jmlee)
	_, doExist := common.AddrToKey[addr]
	if !doExist && addr != common.ZeroAddress {
		newAddrKey := common.HexToHash(strconv.FormatInt(s.NextKey, 16))
		// fmt.Println("make new account -> addr:", addr.Hex(), "/ keyHash:", newAddrKey)
		s.AddrToKeyDirty[addr] = newAddrKey
		s.NextKey += 1
	}

	// (joonha)
	// prev is the deleted object that would be converted into the active object
	// get the object from the inactiveTrie
	prev = s.getDeletedStateObject(addr, 1) // Note, prev might have been deleted, we need that!

	var prevdestruct bool
	if s.snap_inactive != nil && prev != nil {
		if common.UsingInactiveStorageSnapshot {
			_, prevdestruct = s.snapDestructs_inactive[prev.addrHash]
			if !prevdestruct {
				s.snapDestructs_inactive[prev.addrHash] = struct{}{}
			}
		}
	}
	// newobj = newObject(s, addr, Account{})
	newobj = newObject(s, addr, types.StateAccount{}) // v1.10.16
	newobj.setNonce(0) // sets the object to dirty
	if prev == nil {
		s.journal.append(createObjectChange{account: &addr})
	} else {
		s.journal.append(resetObjectChange{prev: prev, prevdestruct: prevdestruct})
	}
	s.setStateObject(newobj)
	if prev != nil && !prev.deleted {
		return newobj, prev
	}
	return newobj, nil
}

// CreateAccount explicitly creates a state object. If a state object with the address
// already exists the balance is carried over to the new account.
//
// CreateAccount is called during the EVM CREATE operation. The situation might arise that
// a contract does the following:
//
//   1. sends funds to sha(account ++ (nonce + 1))
//   2. tx_create(sha(account ++ nonce)) (note that this gets the address of 1)
//
// Carrying over the balance ensures that Ether doesn't disappear.
func (s *StateDB) CreateAccount(addr common.Address) {
	// newObj, prev := s.createObject(addr) // --> original code
	newObj, prev := s.createObject(addr, nil) // (joonha)
	if prev != nil {
		newObj.setBalance(prev.data.Balance)
	}
}

// (joonha)
func (s *StateDB) CreateAccount_withBlockNum(addr common.Address, blockNum *big.Int) {
	newObj, prev := s.createObject(addr, blockNum)
	if prev != nil { // previous account in Active trie
		newObj.setBalance(prev.data.Balance)
	}
}

// CreateAccount_restoring restores account by creation (joonha)
func (s *StateDB) CreateAccount_restoring(addr common.Address) {
	newObj, prev := s.createObject_restoring(addr)
	if prev != nil {
		newObj.setBalance(prev.data.Balance)
	}
}

func (db *StateDB) ForEachStorage(addr common.Address, cb func(key, value common.Hash) bool) error {
	so := db.getStateObject(addr)
	if so == nil {
		return nil
	}
	it := trie.NewIterator(so.getTrie(db.db).NodeIterator(nil))

	for it.Next() {
		key := common.BytesToHash(db.trie.GetKey(it.Key))
		if value, dirty := so.dirtyStorage[key]; dirty {
			if !cb(key, value) {
				return nil
			}
			continue
		}

		if len(it.Value) > 0 {
			_, content, _, err := rlp.Split(it.Value)
			if err != nil {
				return err
			}
			if !cb(key, common.BytesToHash(content)) {
				return nil
			}
		}
	}
	return nil
}

// Copy creates a deep, independent copy of the state.
// Snapshots of the copied state cannot be applied to the copy.
func (s *StateDB) Copy() *StateDB {
	// Copy all the basic fields, initialize the memory ones
	state := &StateDB{
		db:                  s.db,
		trie:                s.db.CopyTrie(s.trie),
		stateObjects:        make(map[common.Address]*stateObject, len(s.journal.dirties)),
		stateObjectsPending: make(map[common.Address]struct{}, len(s.stateObjectsPending)),
		stateObjectsDirty:   make(map[common.Address]struct{}, len(s.journal.dirties)),
		refund:              s.refund,
		logs:                make(map[common.Hash][]*types.Log, len(s.logs)),
		logSize:             s.logSize,
		preimages:           make(map[common.Hash][]byte, len(s.preimages)),
		journal:             newJournal(),
		hasher:              crypto.NewKeccakState(),
		// (jmlee)
		NextKey:		 	 s.NextKey,
		CheckpointKey: 		 s.CheckpointKey,
		AddrToKeyDirty:		 make(map[common.Address]common.Hash, len(s.AddrToKeyDirty)),
		KeysToDeleteDirty:	 make([]common.Hash, len(s.KeysToDeleteDirty)),
		// (joonha)
		AlreadyRestoredDirty:make(map[common.Hash]common.Empty, len(s.AlreadyRestoredDirty)),
		AddrToKeyDirty_inactive:	make(map[common.Address][]common.Hash, len(s.AddrToKeyDirty_inactive)),

	}

	// Copy the dirty states, logs, and preimages
	// (jmlee)
	for key, value := range s.AddrToKeyDirty {
		state.AddrToKeyDirty[key] = value
	}
	// (joonha)
	for key, value := range s.AlreadyRestoredDirty {
		state.AlreadyRestoredDirty[key] = value
	}
	// (joonha)
	for key, _ := range s.AddrToKeyDirty_inactive {
		// deep copy
		for idx, value := range s.AddrToKeyDirty_inactive[key] {
			_, doExist := state.AddrToKeyDirty_inactive[key]
			if !doExist {
				state.AddrToKeyDirty_inactive[key] = make([]common.Hash, len(s.AddrToKeyDirty_inactive[key]))
			}
			state.AddrToKeyDirty_inactive[key][idx] = value
		}
		if len(s.AddrToKeyDirty_inactive[key]) > 0 { 
			state.AddrToKeyDirty_inactive[key] = state.AddrToKeyDirty_inactive[key][:len(s.AddrToKeyDirty_inactive[key])]
		}

		_, ok := s.AddrToKeyDirty_inactive[key];
		if !ok { // empty map
			delete(state.AddrToKeyDirty_inactive, key);
		}
	}
	for i := 0; i < len(s.KeysToDeleteDirty); i++ {
		state.KeysToDeleteDirty[i] = s.KeysToDeleteDirty[i]
	}
	for addr := range s.journal.dirties {
		// As documented [here](https://github.com/ethereum/go-ethereum/pull/16485#issuecomment-380438527),
		// and in the Finalise-method, there is a case where an object is in the journal but not
		// in the stateObjects: OOG after touch on ripeMD prior to Byzantium. Thus, we need to check for
		// nil
		if object, exist := s.stateObjects[addr]; exist {
			// Even though the original object is dirty, we are not copying the journal,
			// so we need to make sure that anyside effect the journal would have caused
			// during a commit (or similar op) is already applied to the copy.
			state.stateObjects[addr] = object.deepCopy(state)

			state.stateObjectsDirty[addr] = struct{}{}   // Mark the copy dirty to force internal (code/state) commits
			state.stateObjectsPending[addr] = struct{}{} // Mark the copy pending to force external (account) commits
		}
	}
	// Above, we don't copy the actual journal. This means that if the copy is copied, the
	// loop above will be a no-op, since the copy's journal is empty.
	// Thus, here we iterate over stateObjects, to enable copies of copies
	for addr := range s.stateObjectsPending {
		if _, exist := state.stateObjects[addr]; !exist {
			state.stateObjects[addr] = s.stateObjects[addr].deepCopy(state)
		}
		state.stateObjectsPending[addr] = struct{}{}
	}
	for addr := range s.stateObjectsDirty {
		if _, exist := state.stateObjects[addr]; !exist {
			state.stateObjects[addr] = s.stateObjects[addr].deepCopy(state)
		}
		state.stateObjectsDirty[addr] = struct{}{}
	}
	for hash, logs := range s.logs {
		cpy := make([]*types.Log, len(logs))
		for i, l := range logs {
			cpy[i] = new(types.Log)
			*cpy[i] = *l
		}
		state.logs[hash] = cpy
	}
	for hash, preimage := range s.preimages {
		state.preimages[hash] = preimage
	}
	// Do we need to copy the access list? In practice: No. At the start of a
	// transaction, the access list is empty. In practice, we only ever copy state
	// _between_ transactions/blocks, never in the middle of a transaction.
	// However, it doesn't cost us much to copy an empty list, so we do it anyway
	// to not blow up if we ever decide copy it in the middle of a transaction
	state.accessList = s.accessList.Copy()

	// If there's a prefetcher running, make an inactive copy of it that can
	// only access data but does not actively preload (since the user will not
	// know that they need to explicitly terminate an active copy).
	if s.prefetcher != nil {
		state.prefetcher = s.prefetcher.copy()
	}
	if s.snaps != nil {
		// In order for the miner to be able to use and make additions
		// to the snapshot tree, we need to copy that aswell.
		// Otherwise, any block mined by ourselves will cause gaps in the tree,
		// and force the miner to operate trie-backed only
		state.snaps = s.snaps
		state.snap = s.snap
		// deep copy needed
		state.snapDestructs = make(map[common.Hash]struct{})
		for k, v := range s.snapDestructs {
			state.snapDestructs[k] = v
		}
		state.snapAccounts = make(map[common.Hash][]byte)
		for k, v := range s.snapAccounts {
			state.snapAccounts[k] = v
		}
		state.snapStorage = make(map[common.Hash]map[common.Hash][]byte)
		for k, v := range s.snapStorage {
			temp := make(map[common.Hash][]byte)
			for kk, vv := range v {
				temp[kk] = vv
			}
			state.snapStorage[k] = temp
		}
	}
	// do it also for the inactive snapshot (joonha)
	if s.snaps_inactive != nil {
		state.snaps_inactive = s.snaps_inactive
		state.snap_inactive = s.snap_inactive
		// deep copy needed
		state.snapDestructs_inactive = make(map[common.Hash]struct{})
		for k, v := range s.snapDestructs_inactive {
			state.snapDestructs_inactive[k] = v
		}
		state.snapAccounts_inactive = make(map[common.Hash][]byte)
		for k, v := range s.snapAccounts_inactive {
			state.snapAccounts_inactive[k] = v 
		}
		state.snapStorage_inactive = make(map[common.Hash]map[common.Hash][]byte)
		for k, v := range s.snapStorage_inactive {
			temp := make(map[common.Hash][]byte)
			for kk, vv := range v {
				temp[kk] = vv
			}
			state.snapStorage_inactive[k] = temp
		}
	}
	
	return state
}

// Snapshot returns an identifier for the current revision of the state.
func (s *StateDB) Snapshot() int {
	id := s.nextRevisionId
	s.nextRevisionId++
	s.validRevisions = append(s.validRevisions, revision{id, s.journal.length()})
	return id
}

// RevertToSnapshot reverts all state changes made since the given revision.
func (s *StateDB) RevertToSnapshot(revid int) {
	// Find the snapshot in the stack of valid snapshots.
	idx := sort.Search(len(s.validRevisions), func(i int) bool {
		return s.validRevisions[i].id >= revid
	})
	if idx == len(s.validRevisions) || s.validRevisions[idx].id != revid {
		panic(fmt.Errorf("revision id %v cannot be reverted", revid))
	}
	snapshot := s.validRevisions[idx].journalIndex

	// Replay the journal to undo changes and remove invalidated snapshots
	s.journal.revert(s, snapshot)
	s.validRevisions = s.validRevisions[:idx]
}

// GetRefund returns the current value of the refund counter.
func (s *StateDB) GetRefund() uint64 {
	return s.refund
}

// Finalise finalises the state by removing the s destructed objects and clears
// the journal as well as the refunds. Finalise, however, will not push any updates
// into the tries just yet. Only IntermediateRoot or Commit will do that.
func (s *StateDB) Finalise(deleteEmptyObjects bool) {
	addressesToPrefetch := make([][]byte, 0, len(s.journal.dirties))
	for addr := range s.journal.dirties {
		obj, exist := s.stateObjects[addr]
		if !exist {
			// ripeMD is 'touched' at block 1714175, in tx 0x1237f737031e40bcde4a8b7e717b2d15e3ecadfe49bb1bbc71ee9deb09c6fcf2
			// That tx goes out of gas, and although the notion of 'touched' does not exist there, the
			// touch-event will still be recorded in the journal. Since ripeMD is a special snowflake,
			// it will persist in the journal even though the journal is reverted. In this special circumstance,
			// it may exist in `s.journal.dirties` but not in `s.stateObjects`.
			// Thus, we can safely ignore it here
			continue
		}
		if obj.suicided || (deleteEmptyObjects && obj.empty()) {
			obj.deleted = true

			// If state snapshotting is active, also mark the destruction there.
			// Note, we can't do this only at the end of a block because multiple
			// transactions within the same block might self destruct and then
			// ressurrect an account; but the snapshotter needs both events.
			if s.snap != nil {
				s.snapDestructs[obj.addrHash] = struct{}{} // We need to maintain account deletions explicitly (will remain set indefinitely)
				delete(s.snapAccounts, obj.addrHash)       // Clear out any previously updated account data (may be recreated via a ressurrect)
				delete(s.snapStorage, obj.addrHash)        // Clear out any previously updated storage data (may be recreated via a ressurrect)
			}
		} else {
			obj.finalise(true) // Prefetch slots in the background
		}
		s.stateObjectsPending[addr] = struct{}{}
		s.stateObjectsDirty[addr] = struct{}{}

		// At this point, also ship the address off to the precacher. The precacher
		// will start loading tries, and when the change is eventually committed,
		// the commit-phase will be a lot faster
		addressesToPrefetch = append(addressesToPrefetch, common.CopyBytes(addr[:])) // Copy needed for closure
	}
	if s.prefetcher != nil && len(addressesToPrefetch) > 0 {
		s.prefetcher.prefetch(s.originalRoot, addressesToPrefetch)
	}
	// Invalidate journal because reverting across transactions is not allowed.
	s.clearJournalAndRefund()
}

// IntermediateRoot computes the current root hash of the state trie.
// It is called in between transactions to get the root hash that
// goes into transaction receipts.
func (s *StateDB) IntermediateRoot(deleteEmptyObjects bool) common.Hash {
	// Finalise all the dirty storage states and write them into the tries
	s.Finalise(deleteEmptyObjects)

	// If there was a trie prefetcher operating, it gets aborted and irrevocably
	// modified after we start retrieving tries. Remove it from the statedb after
	// this round of use.
	//
	// This is weird pre-byzantium since the first tx runs with a prefetcher and
	// the remainder without, but pre-byzantium even the initial prefetcher is
	// useless, so no sleep lost.
	prefetcher := s.prefetcher
	if s.prefetcher != nil {
		defer func() {
			s.prefetcher.close()
			s.prefetcher = nil
		}()
	}
	// Although naively it makes sense to retrieve the account trie and then do
	// the contract storage and account updates sequentially, that short circuits
	// the account prefetcher. Instead, let's process all the storage updates
	// first, giving the account prefeches just a few more milliseconds of time
	// to pull useful data from disk.
	for addr := range s.stateObjectsPending {
		if obj := s.stateObjects[addr]; !obj.deleted {
			obj.updateRoot(s.db)
		}
	}
	// Now we're about to start to write changes to the trie. The trie is so far
	// _untouched_. We can check with the prefetcher, if it can give us a trie
	// which has the same root, but also has some content loaded into it.
	// // --> (joonha) comment-out the original code (this hinders inactive node to be restored when snapshot is on)
	// if prefetcher != nil {
	// 	if trie := prefetcher.trie(s.originalRoot); trie != nil {
	// 		s.trie = trie
	// 	}
	// }

	// so activate this prefetching only when snapshot is off (joonha)
	if s.snap == nil {
		if prefetcher != nil {
			if trie := prefetcher.trie(s.originalRoot); trie != nil {
				s.trie = trie
			}
		}
	}

	usedAddrs := make([][]byte, 0, len(s.stateObjectsPending))
	for addr := range s.stateObjectsPending {
		if obj := s.stateObjects[addr]; obj.deleted {
			s.deleteStateObject(obj)
			s.AccountDeleted += 1
		} else {
			s.updateStateObject(obj)
			s.AccountUpdated += 1
		}
		usedAddrs = append(usedAddrs, common.CopyBytes(addr[:])) // Copy needed for closure
	}
	if prefetcher != nil {
		prefetcher.used(s.originalRoot, usedAddrs)
	}
	if len(s.stateObjectsPending) > 0 {
		s.stateObjectsPending = make(map[common.Address]struct{})
	}
	// Track the amount of time wasted on hashing the account trie
	if metrics.EnabledExpensive {
		defer func(start time.Time) { s.AccountHashes += time.Since(start) }(time.Now())
	}
	return s.trie.Hash()
}

// Prepare sets the current transaction hash and index which are
// used when the EVM emits new state logs.
func (s *StateDB) Prepare(thash common.Hash, ti int) {
	s.thash = thash
	s.txIndex = ti
	s.accessList = newAccessList()
}

func (s *StateDB) clearJournalAndRefund() {
	if len(s.journal.entries) > 0 {
		s.journal = newJournal()
		s.refund = 0
	}
	s.validRevisions = s.validRevisions[:0] // Snapshots can be created without journal entires
}

// Commit writes the state to the underlying in-memory trie database.
func (s *StateDB) Commit(deleteEmptyObjects bool) (common.Hash, error) {
	if s.dbErr != nil {
		return common.Hash{}, fmt.Errorf("commit aborted due to earlier error: %v", s.dbErr)
	}
	// Finalize any pending changes and merge everything into the tries
	s.IntermediateRoot(deleteEmptyObjects)

	// Commit objects to the trie, measuring the elapsed time
	var storageCommitted int
	codeWriter := s.db.TrieDB().DiskDB().NewBatch()
	for addr := range s.stateObjectsDirty {
		if obj := s.stateObjects[addr]; !obj.deleted {
			// maybe this updates storage tries (jmlee)
			// Write any contract code associated with the state object
			if obj.code != nil && obj.dirtyCode {
				rawdb.WriteCode(codeWriter, common.BytesToHash(obj.CodeHash()), obj.code)
				obj.dirtyCode = false
			}
			// Write any storage changes in the state object to its storage trie
			committed, err := obj.CommitTrie(s.db)
			if err != nil {
				return common.Hash{}, err
			}
			storageCommitted += committed
			// // printing storage trie (joonha)
			// fmt.Println("\nPrint_storageTrie starts")
			// fmt.Println("( account addr: ", obj.address, ")")
			// fmt.Println("( account addrHash: ", obj.addrHash, ")")
			// obj.Print_storageTrie()
			// fmt.Println("Print_storageTrie ends\n")
		}
		// // codes for debugging (joonha)
		// var slotKeyList []common.Hash
		// for slotKey, _ := range s.snapStorage[s.stateObjects[addr].addrHash] {
		// 	obj := s.stateObjects[addr]
		// 	trieSlotValue := obj.GetCommittedState_hashedKey(s.db, slotKey)
		// 	fmt.Println("trieSlotValue: ", trieSlotValue)
		// 	slotKeyList = append(slotKeyList, slotKey)
		// } 
		// fmt.Println("[StorageTrie(addr)]: ", s.StorageTrie(addr))
		// fmt.Println("[addr]: ", addr)
		// fmt.Println("[slotKeyList]: ", slotKeyList)
		// fmt.Println("[CodeHash]: ", s.stateObjects[addr].data.CodeHash)
		// fmt.Println("[Root]: ", s.stateObjects[addr].data.Root)
	}

	// apply dirties to common.AddrToKey (jmlee)
	common.AddrToKeyMapMutex.Lock()
	for key, value := range s.AddrToKeyDirty {
		common.AddrToKey[key] = value
	}
	common.AddrToKeyMapMutex.Unlock()

	// apply dirties to common.AlreadyRestored (joonha)
	// fmt.Println("len(s.AlreadyRestoredDirty): ", len(s.AlreadyRestoredDirty))
	for key, _ := range s.AlreadyRestoredDirty {
		common.AlreadyRestored[key] = common.Empty{}
	}

	// apply dirties to common.AddrToKey_inactive (joonha)
	// fmt.Println("len(s.AddrToKeyDirty_inactive): ", len(s.AddrToKeyDirty_inactive))
	for key, _ := range s.AddrToKeyDirty_inactive {
		prevLen := 0
		_, doExist_1 := common.AddrToKey_inactive[key]
		if doExist_1 {
			prevLen = len(common.AddrToKey_inactive[key])
		} else {
			prevLen = 1
		}

		for _, value := range s.AddrToKeyDirty_inactive[key] {
			_, doExist := common.AddrToKey_inactive[key]
			if !doExist {
				common.AddrToKey_inactive[key] = make([]common.Hash, len(s.AddrToKeyDirty_inactive[key]))
				// fmt.Println("addr: ", key)
			}
			// fmt.Println("length: ", len(common.AddrToKey_inactive[key]))
			common.AddrToKey_inactive[key] = append(common.AddrToKey_inactive[key], value)
		}
		common.AddrToKey_inactive[key] = common.AddrToKey_inactive[key][prevLen:]

		orig_len := len(s.AddrToKeyDirty_inactive[key])
		if orig_len > 0 { 
			for i := 0; i < orig_len; i++ {
				for j := 0; j < len(s.AddrToKeyDirty_inactive[key]); j++ {
					if s.AddrToKeyDirty_inactive[key][j] == common.ToBeDeletedKey {
						s.AddrToKeyDirty_inactive[key] = append(s.AddrToKeyDirty_inactive[key][:j], s.AddrToKeyDirty_inactive[key][j+1:]...)
						common.AddrToKey_inactive[key] = append(common.AddrToKey_inactive[key][:j], common.AddrToKey_inactive[key][j+1:]...)
						break;
					}
				}
			}
		}

		if len(s.AddrToKeyDirty_inactive[key]) == 0 {
			delete(common.AddrToKey_inactive, key);
		}

		// fmt.Println("s.AddrToKeyDirty_inactive[",key,"]: ", s.AddrToKeyDirty_inactive[key])
		// fmt.Println("common.AddrToKey_inactive[",key,"]: ", common.AddrToKey_inactive[key])
	}
	// fmt.Println("\nlen(common.AddrToKey_inactive): ", len(common.AddrToKey_inactive))




	// apply dirties to common.KeysToDelete (jmlee)
	// for i := 0; i < len(s.KeysToDeleteDirty); i++ {
	// 	fmt.Println("s.KeysToDeleteDirty[",i,"]:", s.KeysToDeleteDirty[i])
	// }
	common.KeysToDelete = append(common.KeysToDelete, s.KeysToDeleteDirty...)
	// for i := 0; i < len(common.KeysToDelete); i++ {
	// 	fmt.Println("common.KeysToDelete[",i,"]:", common.KeysToDelete[i])
	// }

	if len(s.stateObjectsDirty) > 0 {
		s.stateObjectsDirty = make(map[common.Address]struct{})
	}
	if codeWriter.ValueSize() > 0 {
		if err := codeWriter.Write(); err != nil {
			log.Crit("Failed to commit dirty codes", "error", err)
		}
	}
	// Write the account trie changes, measuing the amount of wasted time
	var start time.Time
	if metrics.EnabledExpensive {
		start = time.Now()
	}

	// // decide whether to delete leaf nodes or not (jmlee) --> commented-out by (joonha) cuz it occurs an err and it's already done in fillTransactions
	// // this might be needed for full sync
	// if common.DoDeleteLeafNode {
	// 	// delete previous leaf nodes from state trie
	// 	s.DeletePreviousLeafNodes(common.KeysToDelete)

	// 	// reset common.KeysToDelete --> commented-out by (joonha) cuz it occurs an err and it's already done in fillTransactions
	// 	common.KeysToDelete = make([]common.Hash, 0) // only works at the epoch
	// }

	// 여기서도 state trie에 delete와 inactivate가 적용되지 않은 상태인지 확인하기 위해 프린팅을 해보는 것임 (joonha)
	// fmt.Println("\n\n(3) s.db.TrieDB(): ", s.db.TrieDB(), "\n\n")

	// Here, writing to TrieDB is occurred (joonha) (이곳을 기준으로 (3)과 (4)의 프린팅에 차이가 있기 때문임.)

	// The onleaf func is called _serially_, so we can reuse the same account
	// for unmarshalling every time.
	var account types.StateAccount
	root, accountCommitted, err := s.trie.Commit(func(_ [][]byte, _ []byte, leaf []byte, parent common.Hash) error {
		if err := rlp.DecodeBytes(leaf, &account); err != nil {
			return nil
		}
		if account.Root != emptyRoot {
			s.db.TrieDB().Reference(account.Root, parent)
		}
		return nil
	})
	if err != nil {
		return common.Hash{}, err
	}

	// 여기서도 state trie에 delete와 inactivate가 적용되지 않은 상태인지 확인하기 위해 프린팅을 해보는 것임 (joonha)
	// fmt.Println("\n\n(4) s.db.TrieDB(): ", s.db.TrieDB(), "\n\n")

	if metrics.EnabledExpensive {
		s.AccountCommits += time.Since(start)

		accountUpdatedMeter.Mark(int64(s.AccountUpdated))
		storageUpdatedMeter.Mark(int64(s.StorageUpdated))
		accountDeletedMeter.Mark(int64(s.AccountDeleted))
		storageDeletedMeter.Mark(int64(s.StorageDeleted))
		accountCommittedMeter.Mark(int64(accountCommitted))
		storageCommittedMeter.Mark(int64(storageCommitted))
		s.AccountUpdated, s.AccountDeleted = 0, 0
		s.StorageUpdated, s.StorageDeleted = 0, 0
	}
	// If snapshotting is enabled, update the snapshot tree with this new version
	if s.snap != nil {
		if metrics.EnabledExpensive {
			defer func(start time.Time) { s.SnapshotCommits += time.Since(start) }(time.Now())
		}
		// Only update if there's a state transition (skip empty Clique blocks)
		if parent := s.snap.Root(); parent != root {

			// // code for debugging (jmlee)
			// for k, v := range s.snapAccounts {
			// 	fmt.Println("\ns.snapAccounts -> k:", k, "/ v:", v)
			// 	// (joonha)
			// 	for slotKey, slotValue := range s.snapStorage[k] {
			// 		fmt.Println("\ts.snapStorage -> slotKey:", slotKey, "/ slotValue: ", slotValue)
			// 	} 
			// }

			if err := s.snaps.Update(root, parent, s.snapDestructs, s.snapAccounts, s.snapStorage); err != nil {
				log.Warn("Failed to update snapshot tree", "from", parent, "to", root, "err", err)
			}
			// Keep 128 diff layers in the memory, persistent layer is 129th.
			// - head layer is paired with HEAD state
			// - head-1 layer is paired with HEAD-1 state
			// - head-127 layer(bottom-most diff layer) is paired with HEAD-127 state
			if err := s.snaps.Cap(root, 128); err != nil {
				log.Warn("Failed to cap snapshot tree", "root", root, "layers", 128, "err", err)
			}
		}
		s.snap, s.snapDestructs, s.snapAccounts, s.snapStorage = nil, nil, nil, nil
	}

	// update inactive storage snapshot (joonha)
	if s.snap_inactive != nil {
		if common.UsingInactiveStorageSnapshot {
			// s.snap_inactive = s.snaps_inactive.Snapshot(root) 
			if s.snap_inactive != nil { 

				// // code for debugging
				// for k, v := range s.snapAccounts_inactive {
				// 	fmt.Println("s.snapAccounts_inactive -> k:", k, "/ v:", v)
				// }
				// for k, v := range s.snapStorage_inactive {
				// 	for kk, vv := range v {
				// 		fmt.Println("s.snapStorage_inactive[", k, "] -> kk:", kk, "/ vv:", vv)
				// 	} 
				// }

				if parent_inactive := s.snap_inactive.Root(); parent_inactive != root {
					// TODO(joonha): snapAccount_inactive는 없을테니 추후 저장 효율을 위해 nil을 넘겨줄 것.
					if err := s.snaps_inactive.Update(root, parent_inactive, s.snapDestructs_inactive, s.snapAccounts_inactive, s.snapStorage_inactive); err != nil {
						log.Warn("Failed to update snapshot tree", "from", parent_inactive, "to", root, "err", err)
					}
					if err := s.snaps_inactive.Cap(root, 128); err != nil {
						log.Warn("Failed to cap snapshot tree", "root", root, "layers", 128, "err", err)
					}
				}
			} else {
				// fmt.Println("s.snap_inactive is nil")
			}

			s.snap_inactive, s.snapDestructs_inactive, s.snapAccounts_inactive, s.snapStorage_inactive = nil, nil, nil, nil
		}
	}

	return root, err
}

// PrepareAccessList handles the preparatory steps for executing a state transition with
// regards to both EIP-2929 and EIP-2930:
//
// - Add sender to access list (2929)
// - Add destination to access list (2929)
// - Add precompiles to access list (2929)
// - Add the contents of the optional tx access list (2930)
//
// This method should only be called if Berlin/2929+2930 is applicable at the current number.
func (s *StateDB) PrepareAccessList(sender common.Address, dst *common.Address, precompiles []common.Address, list types.AccessList) {
	s.AddAddressToAccessList(sender)
	if dst != nil {
		s.AddAddressToAccessList(*dst)
		// If it's a create-tx, the destination will be added inside evm.create
	}
	for _, addr := range precompiles {
		s.AddAddressToAccessList(addr)
	}
	for _, el := range list {
		s.AddAddressToAccessList(el.Address)
		for _, key := range el.StorageKeys {
			s.AddSlotToAccessList(el.Address, key)
		}
	}
}

// AddAddressToAccessList adds the given address to the access list
func (s *StateDB) AddAddressToAccessList(addr common.Address) {
	if s.accessList.AddAddress(addr) {
		s.journal.append(accessListAddAccountChange{&addr})
	}
}

// AddSlotToAccessList adds the given (address, slot)-tuple to the access list
func (s *StateDB) AddSlotToAccessList(addr common.Address, slot common.Hash) {
	addrMod, slotMod := s.accessList.AddSlot(addr, slot)
	if addrMod {
		// In practice, this should not happen, since there is no way to enter the
		// scope of 'address' without having the 'address' become already added
		// to the access list (via call-variant, create, etc).
		// Better safe than sorry, though
		s.journal.append(accessListAddAccountChange{&addr})
	}
	if slotMod {
		s.journal.append(accessListAddSlotChange{
			address: &addr,
			slot:    &slot,
		})
	}
}

// AddressInAccessList returns true if the given address is in the access list.
func (s *StateDB) AddressInAccessList(addr common.Address) bool {
	return s.accessList.ContainsAddress(addr)
}

// SlotInAccessList returns true if the given (address, slot)-tuple is in the access list.
func (s *StateDB) SlotInAccessList(addr common.Address, slot common.Hash) (addressPresent bool, slotPresent bool) {
	return s.accessList.Contains(addr, slot)
}

// DeletePreviousLeafNodes deletes previous leaf nodes from state trie (jmlee)
func (s *StateDB) DeletePreviousLeafNodes(keysToDelete []common.Hash) {
	// fmt.Println("\ntrie root before delete leaf nodes:", s.trie.Hash().Hex())
	// delete previous leaf nodes from state trie
	for i := 0; i < len(keysToDelete); i++ {
		// fmt.Println("delete previous leaf node -> key:", keysToDelete[i].Hex())
		if err := s.trie.TryUpdate_SetKey(keysToDelete[i][:], nil); err != nil {
			s.setError(fmt.Errorf("updateStateObject (%x) error: %v", keysToDelete[i][:], err))
		}
	}
	// fmt.Println("trie root after delete leaf nodes:", s.trie.Hash().Hex())

}

// InactivateLeafNodes inactivates inactive accounts (i.e., move old leaf nodes to left) (jmlee)
func (s *StateDB) InactivateLeafNodes(firstKeyToCheck, lastKeyToCheck int64) int64 {

	// fmt.Println("/******************************/")
	// fmt.Println("InactivateLeafNodes")
	// fmt.Println("/******************************/")

	// fmt.Println("\ninspect trie to inactivate:", firstKeyToCheck, "~",lastKeyToCheck)
	// fmt.Println("trie root before inactivate leaf nodes:", s.trie.Hash().Hex())

	// normTrie := s.trie.GetTrie() // TODO: using this function, we can delete SecureTrie.***_SetKey functions

	// DFS the non-nil account from the trie (joonha)
	firstKey := common.Int64ToHash(firstKeyToCheck)
	lastKey := common.Int64ToHash(lastKeyToCheck)
	AccountsToInactivate, KeysToInactivate, _ := s.trie.TryGetAll_SetKey(firstKey[:], lastKey[:])

	// fmt.Println("")
	// fmt.Println("Accounts length: ", len(AccountsToInactivate))
	// fmt.Println("Keys length: ", len(KeysToInactivate))
	// // fmt.Println("AccountsToInactivate: ", AccountsToInactivate)
	// // fmt.Println("KeysToInactivate: ", KeysToInactivate)

	// move inactive leaf nodes to left
	for index, key := range KeysToInactivate { 

		// to delete storage trie from disk
		addr := common.BytesToAddress(AccountsToInactivate[index])
		common.AccountsToDeleteFromDisk = append(common.AccountsToDeleteFromDisk, addr)

		// insert inactive leaf node to left
		keyToInsert := common.Int64ToHash(common.InactiveBoundaryKey + int64(index))
		// fmt.Println("(Inactivate)insert -> key:", keyToInsert.Hex())
		if err := s.trie.TryUpdate_SetKey(keyToInsert[:], AccountsToInactivate[index]); err != nil {
			s.setError(fmt.Errorf("updateStateObject (%x) error: %v", keyToInsert[:], err))
		} else {

			// apply inactivation result to AddrToKey 
			// Q. Should this be applied to AddrToKey_Dirty?
			common.AddrToKey[common.BytesToAddress(AccountsToInactivate[index])] = keyToInsert
			s.AddrToKeyDirty[common.BytesToAddress(AccountsToInactivate[index])] = keyToInsert

			// save the inactive account key info for later restoration 
			_, doExist := s.AddrToKeyDirty_inactive[common.BytesToAddress(AccountsToInactivate[index])]
			if !doExist {
				s.AddrToKeyDirty_inactive[common.BytesToAddress(AccountsToInactivate[index])] = common.AddrToKey_inactive[common.BytesToAddress(AccountsToInactivate[index])]
			}
			s.AddrToKeyDirty_inactive[common.BytesToAddress(AccountsToInactivate[index])] = append(s.AddrToKeyDirty_inactive[common.BytesToAddress(AccountsToInactivate[index])], keyToInsert)

			// fmt.Println("s.AddrToKeyDirty_inactive is ", s.AddrToKeyDirty_inactive[common.BytesToAddress(AccountsToInactivate[index])])
			// fmt.Println("acc: ", common.BytesToAddress(AccountsToInactivate[index]))
		}


		/*****************************/
		// SNAPSHOT
		/*****************************/
		// // ACCOUNT 
		// // we don't need inactive account snapshot, but in case of storage depending on account, move accounts to inactive snapshot Tree
		// // if commenting this part out doesn't occur err, comment out for memory optimization.
		// // s.snapAccounts_inactive = make(map[common.Hash][]byte)
		// acc, _ := s.snap.AccountRLP(key)
		// // fmt.Println("### acc: ", acc)
		// s.snapAccounts_inactive[keyToInsert] = acc
		// // fmt.Println("s.snapAccounts_inactive[keyToInsert]: ", s.snapAccounts_inactive[keyToInsert])
		// // fmt.Println(">>> key is ", keyToInsert)

		// STORAGE
		if common.UsingInactiveStorageSnapshot {

			// accountList := s.snaps.AccountList_ethane(snapRoot)
			// fmt.Println("accountList: ", accountList) // key

			if common.UsingActiveSnapshot { // when activeSnapshot is on, get slotKeyList from actvie snapshot

				snapRoot := s.snap.Root()

				// get slotKeyList from active snapshot
				slotKeyList := s.snaps.StorageList_ethane(snapRoot, key) // active snapshot's storage list
				// fmt.Println("slotKeyList: ", slotKeyList)
				temp := make(map[common.Hash][]byte)
				for _, slotKey := range slotKeyList {
					// fmt.Println("slotKey is ", slotKey)
					v, _ := s.snap.Storage(key, slotKey) 
					// fmt.Println("v is", v)
					temp[slotKey] = v
				}
				s.snapStorage_inactive[keyToInsert] = temp	

				// DELETE
				s.snapDestructs[key] = struct{}{}
				delete(s.snapAccounts, key)
				delete(s.snapStorage, key)

			} else if common.UsingActiveSnapshot == false { // get slotKeyList from object's storage trie			
				obj := s.getStateObject(addr)
				slots := make(map[common.Hash][]byte)

				if obj != nil {
					storageTrie := obj.getTrie(s.db)
					// fmt.Println("storageTrie: ", storageTrie)
					obj.Print_storageTrie() // this printing should be after getTrie()

					slots, _ = storageTrie.TryGetAllSlots()
					s.snapStorage_inactive[keyToInsert] = slots
					// fmt.Println("slots: ", slots)

				} else {
					// fmt.Println("obj has been deleted.")
				}
			}

			// // DELETE ACCOUNT FROM TRIE
			// if err := s.trie.TryUpdate_SetKey(key[:], nil); err != nil {
			// 	s.setError(fmt.Errorf("updateStateObject (%x) error: %v", key[:], err))
			// }
		}

		// DELETE ACCOUNT FROM TRIE
		if err := s.trie.TryUpdate_SetKey(key[:], nil); err != nil {
			s.setError(fmt.Errorf("updateStateObject (%x) error: %v", key[:], err))
		}
	}

	// // print result
	// fmt.Println("inactivate", len(KeysToInactivate), "accounts")
	// fmt.Println("trie root after inactivate leaf nodes:", s.trie.Hash().Hex())

	// for index, account := range AccountsToInactivate {
	// 	target := common.HexToAddress("0x2A3e8b070fB0691B090E9326AE95e029A4776ce4")
	// 	if(target == common.BytesToAddress(account)){
	// 		fmt.Println("the target account is inactivated.")
	// 		fmt.Println("[0x2A3e8b070fB0691B090E9326AE95e029A4776ce4] is ", index, "-th elem of AccountsToInactivate")
	// 	}
	// 	// fmt.Println(common.BytesToAddress(account))
	// 	// if(index >= 100){
	// 	// 	break
	// 	// }
	// }

	// return # of inactivated accounts
	return int64(len(KeysToInactivate))
}

// GetAccount returns Account from stateObject (joonha)
func (s *StateDB) GetAccount(addr common.Address) *types.StateAccount {
	return &s.getStateObject(addr).data
}

// first time to be restored, Add it to s.AlreadyRestoredDirty (joonha)
func (s *StateDB) UpdateAlreadyRestoredDirty(inactiveKey common.Hash) {
	_, doExist := s.AlreadyRestoredDirty[inactiveKey]
	if !doExist {
		s.AlreadyRestoredDirty = map[common.Hash]common.Empty{}
	}
	s.AlreadyRestoredDirty[inactiveKey] = common.Empty{}
	// fmt.Println("AlreadyRestoredDirty map length: ", len(s.AlreadyRestoredDirty))
}

// remove restored account from the list s.AddrToKeyDirty_inactive (joonha)
func (s *StateDB) RemoveRestoredKeyFromAddrToKeyDirty_inactive(inactiveAddr common.Address, retrievedKeys []common.Hash) {
	_, doExist := s.AddrToKeyDirty_inactive[inactiveAddr]
	if !doExist {
		s.AddrToKeyDirty_inactive[inactiveAddr] = common.AddrToKey_inactive[inactiveAddr]
	}

	temp := 0
	for i := len(s.AddrToKeyDirty_inactive[inactiveAddr])-1; i >= 0; i-- {
		for j := temp; j < len(retrievedKeys); j++ {
			if s.AddrToKeyDirty_inactive[inactiveAddr][i] == retrievedKeys[j] {
				// fmt.Println("Remove", i, "th elem of AddrToKeyDirty_inactive[", inactiveAddr, "]")
				// fmt.Println("retrievedKeys[",j,"]: ", retrievedKeys[j])
				s.AddrToKeyDirty_inactive[inactiveAddr][i] = common.ToBeDeletedKey
				temp = j
				break;
			}
		}
	}

	// fmt.Println("\n\nlen(s.AddrToKeyDirty_inactive[inactiveAddr]): ", len(s.AddrToKeyDirty_inactive[inactiveAddr]), "\n\n")
}

// rebuild a storage trie when restoring using snapshot (joonha)
func (s *StateDB) RebuildStorageTrieFromSnapshot(snapRoot common.Hash, addr common.Address, accountHash common.Hash) {

	// TODO: verify if the rebuilt trie root is equal to the account's storage root 

	// retrieve storage slots from snapshot
	slotKeyList := s.snaps_inactive.StorageList_ethane(snapRoot, accountHash)
	// fmt.Println("RESTORING STORAGE LIST: ", slotKeyList)	

	// rebuild storage trie
	temp := make(map[common.Hash][]byte)
	for _, slotKey := range slotKeyList {
		// fmt.Println("slotKey is ", slotKey)
		v, _ := s.snap_inactive.Storage(accountHash, slotKey)
		// fmt.Println("v is", v)

		s.SetState_Restore(addr, slotKey, common.BytesToHash(v))
		temp[slotKey] = v
	}
	obj := s.getStateObject(addr) 
	if obj != nil {
		obj.CommitTrie_hashedKey(s.db)
	}

	// delete inactive snapshot
	delete(s.snapStorage_inactive, accountHash)
	delete(s.snapAccounts_inactive, accountHash)
	s.snapDestructs_inactive[accountHash] = struct{}{}
}

// (joonha)
func (s *StateDB) GetTrie(addr common.Address) Trie {
	obj := s.getStateObject_FromInactiveTrie(addr) 
	if obj == nil {
		return nil
	}
	return obj.getTrie(s.db)
} 

// (joonha)
func (s *StateDB) PrintStateTrie() {
	s.trie.Print()
}

// (joonha)
func (s *StateDB) TrieDB() *trie.Database {
	return s.db.TrieDB()
}

func (s *StateDB) Hash() common.Hash {
	return s.trie.Hash()
} 