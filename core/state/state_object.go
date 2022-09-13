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

package state

import (
	"bytes"
	"fmt"
	"io"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/rlp"
)

var emptyCodeHash = crypto.Keccak256(nil)

type Code []byte

func (c Code) String() string {
	return string(c) //strings.Join(Disassemble(c), " ")
}

type Storage map[common.Hash]common.Hash

func (s Storage) String() (str string) {
	for key, value := range s {
		str += fmt.Sprintf("%X : %X\n", key, value)
	}

	return
}

func (s Storage) Copy() Storage {
	cpy := make(Storage)
	for key, value := range s {
		cpy[key] = value
	}

	return cpy
}

// stateObject represents an Ethereum account which is being modified.
//
// The usage pattern is as follows:
// First you need to obtain a state object.
// Account values can be accessed and modified through the object.
// Finally, call CommitTrie to write the modified storage trie into a database.
type stateObject struct {
	address  common.Address
	addrHash common.Hash // hash of ethereum address of the account
	data     types.StateAccount
	db       *StateDB

	txHash common.Hash //jhkim
	// DB error.
	// State objects are used by the consensus core and VM which are
	// unable to deal with database-level errors. Any error that occurs
	// during a database read is memoized here and will eventually be returned
	// by StateDB.Commit.
	dbErr error

	// Write caches.
	trie Trie // storage trie, which becomes non-nil on first access
	code Code // contract bytecode, which gets set when code is loaded

	originStorage  Storage // Storage cache of original entries to dedup rewrites, reset for every transaction
	pendingStorage Storage // Storage entries that need to be flushed to disk, at the end of an entire block
	dirtyStorage   Storage // Storage entries that have been modified in the current transaction execution
	fakeStorage    Storage // Fake storage which constructed by caller for debugging purpose.

	// Cache flags.
	// When an object is marked suicided it will be delete from the trie
	// during the "update" phase of the state transition.
	dirtyCode bool // true if the code was updated
	suicided  bool
	deleted   bool

	// stage1-substate: stateObject.ResearchTouched
	// jhkim: read를 위한 touch와 write를 위한 touch를 구분하자?
	ResearchTouched      map[common.Hash]struct{}
	ResearchTouchedWrite map[common.Hash]struct{}

	TxStorage map[common.Hash]Storage
}

// empty returns whether the account is considered empty.
func (s *stateObject) empty() bool {
	return s.data.Nonce == 0 && s.data.Balance.Sign() == 0 && bytes.Equal(s.data.CodeHash, emptyCodeHash)
}

// newObject creates a state object.
func newObject(db *StateDB, address common.Address, data types.StateAccount) *stateObject {
	if data.Balance == nil {
		data.Balance = new(big.Int)
	}
	if data.CodeHash == nil {
		data.CodeHash = emptyCodeHash
	}
	if data.Root == (common.Hash{}) {
		data.Root = emptyRoot
	}
	return &stateObject{
		db:             db,
		address:        address,
		addrHash:       crypto.Keccak256Hash(address[:]),
		data:           data,
		originStorage:  make(Storage),
		pendingStorage: make(Storage),
		dirtyStorage:   make(Storage),

		txHash: common.Hash{}, //jhkim
		// stage1-substate: init stateObject.ResearchTouched
		ResearchTouched:      make(map[common.Hash]struct{}),
		ResearchTouchedWrite: make(map[common.Hash]struct{}),
		TxStorage:            make(map[common.Hash]Storage),
	}
}

// EncodeRLP implements rlp.Encoder.
func (s *stateObject) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, &s.data)
}

// setError remembers the first non-nil error it is called with.
func (s *stateObject) setError(err error) {
	if s.dbErr == nil {
		s.dbErr = err
	}
}

func (s *stateObject) markSuicided() {
	s.suicided = true
}

func (s *stateObject) touch() {
	s.db.journal.append(touchChange{
		account: &s.address,
	})
	if s.address == ripemd {
		// Explicitly put it in the dirty-cache, which is otherwise generated from
		// flattened journals.
		s.db.journal.dirty(s.address)
	}
}

func (s *stateObject) getTrie(db Database) Trie {
	if s.trie == nil {
		// Try fetching from prefetcher first
		// We don't prefetch empty tries
		if s.data.Root != emptyRoot && s.db.prefetcher != nil {
			// When the miner is creating the pending state, there is no
			// prefetcher
			s.trie = s.db.prefetcher.trie(s.data.Root)
		}
		if s.trie == nil {
			var err error
			s.trie, err = db.OpenStorageTrie(s.addrHash, s.data.Root)
			if err != nil {
				s.trie, _ = db.OpenStorageTrie(s.addrHash, common.Hash{})
				s.setError(fmt.Errorf("can't create storage trie: %v", err))
			}
		}
	}
	return s.trie
}

// GetState retrieves a value from the account storage trie.
func (s *stateObject) GetState(db Database, key common.Hash) common.Hash {
	// stage1-substate: mark keys touched by GetState
	// fmt.Println("  GetState key", key)
	if _, exist := s.ResearchTouched[key]; !exist {

		s.ResearchTouched[key] = struct{}{}
	}

	// If the fake storage is set, only lookup the state here(in the debugging mode)
	if s.fakeStorage != nil {
		tmp := s.fakeStorage[key]
		// fmt.Println("GetState1 /addr:", s.address, "/slotHash:", key, "/value:", tmp)

		return tmp
	}
	// If we have a dirty value for this state entry, return it
	value, dirty := s.dirtyStorage[key]
	if dirty {
		// fmt.Println("    >>>> From dirty read/ blocknumber:", common.GlobalBlockNumber, "tx:", common.GlobalTxHash, "key:", key, "value:", value)
		// fmt.Println("GetState2 /addr:", s.address, "/slotHash:", key, "/value:", value)
		return value
	}
	// Otherwise return the entry's original value
	//jhkim
	tmp := s.GetCommittedState(db, key)
	// fmt.Println("GetState3 /addr:", s.address, "/slotHash:", key, "/value:", tmp)
	return tmp
}

// GetCommittedState retrieves a value from the committed account storage trie.
func (s *stateObject) GetCommittedState(db Database, key common.Hash) common.Hash {
	// If the fake storage is set, only lookup the state here(in the debugging mode)
	if s.fakeStorage != nil {
		return s.fakeStorage[key]
	}
	// If we have a pending write or clean cached, return that
	if value, pending := s.pendingStorage[key]; pending {
		return value
	}
	if value, cached := s.originStorage[key]; cached {
		return value
	}
	// If no live objects are available, attempt to use snapshots
	var (
		enc []byte
		err error
	)
	if s.db.snap != nil {
		// If the object was destructed in *this* block (and potentially resurrected),
		// the storage has been cleared out, and we should *not* consult the previous
		// snapshot about any storage values. The only possible alternatives are:
		//   1) resurrect happened, and new slot values were set -- those should
		//      have been handles via pendingStorage above.
		//   2) we don't have new values, and can deliver empty response back
		if _, destructed := s.db.snapDestructs[s.addrHash]; destructed {
			return common.Hash{}
		}
		start := time.Now()
		enc, err = s.db.snap.Storage(s.addrHash, crypto.Keccak256Hash(key.Bytes()))
		if metrics.EnabledExpensive {
			s.db.SnapshotStorageReads += time.Since(start)
		}
	}
	// If the snapshot is unavailable or reading from it fails, load from the database.
	if s.db.snap == nil || err != nil {
		start := time.Now()
		enc, err = s.getTrie(db).TryGet(key.Bytes()) // jhkim: 캐싱된것 없을때 진짜로 db에서 가져오는 자리

		if metrics.EnabledExpensive {
			s.db.StorageReads += time.Since(start)
		}
		if err != nil {
			s.setError(err)
			return common.Hash{}
		}
	}
	var value common.Hash
	if len(enc) > 0 {
		_, content, _, err := rlp.Split(enc)
		if err != nil {
			s.setError(err)
		}
		value.SetBytes(content)
	}
	// fmt.Println("    >>>> Real storage read/ blocknumber:", common.GlobalBlockNumber, "tx:", common.GlobalTxHash, "key:", key, "value:", value)
	s.originStorage[key] = value
	return value
}

// SetState updates a value in account storage.
func (s *stateObject) SetState(db Database, key, value common.Hash) {
	// If the fake storage is set, put the temporary state update here.
	if s.fakeStorage != nil {
		s.fakeStorage[key] = value
		return
	}

	// If the new value is the same as old, don't set
	prev := s.GetState(db, key) // jhkim: 모든 stateobject의 setstate에는 getstate가 먼저 이루어진다.
	// if value != common.HexToHash("0x0") {
	// 	fmt.Println("   SetState", key, "value:", prev, "->", value)
	// } else {
	// 	fmt.Println("   SetState 0 value", key, "value:", prev, "->", value)
	// }

	if _, exist := s.ResearchTouchedWrite[key]; !exist {
		s.ResearchTouchedWrite[key] = struct{}{}
	}

	if prev == value {
		// delete(common.TxWriteList[common.GlobalTxHash][s.address].Storage, key) // jhkim
		return
	}

	// New value is different, update and journal the change
	s.db.journal.append(storageChange{
		account:  &s.address,
		key:      key,
		prevalue: prev,
	})
	if _, exist := s.TxStorage[common.GlobalTxHash]; !exist {
		s.TxStorage[common.GlobalTxHash] = make(Storage)
	}
	s.TxStorage[common.GlobalTxHash][key] = value

	s.setState(key, value)
}

// SetStorage replaces the entire state storage with the given one.
//
// After this function is called, all original state will be ignored and state
// lookup only happens in the fake state storage.
//
// Note this function should only be used for debugging purpose.
func (s *stateObject) SetStorage(storage map[common.Hash]common.Hash) {
	// Allocate fake storage if it's nil.
	if s.fakeStorage == nil {
		s.fakeStorage = make(Storage)
	}
	for key, value := range storage {
		s.fakeStorage[key] = value
	}
	// Don't bother journal since this function should only be used for
	// debugging and the `fake` storage won't be committed to database.
}

func (s *stateObject) setState(key, value common.Hash) {
	// fmt.Println("      setstate", key, value)
	if _, exist := s.TxStorage[common.GlobalTxHash]; !exist {
		s.TxStorage[common.GlobalTxHash] = make(Storage)
	}
	s.TxStorage[common.GlobalTxHash][key] = value
	s.dirtyStorage[key] = value
	// fmt.Println("      setstate: dirtyStorage", s.dirtyStorage)
}

// finalise moves all dirty storage slots into the pending area to be hashed or
// committed later. It is invoked at the end of every transaction.
func (s *stateObject) finalise(prefetch bool) {
	slotsToPrefetch := make([][]byte, 0, len(s.dirtyStorage))
	for key, value := range s.dirtyStorage {
		// fmt.Println("    stateObject.finalise move key value from dirty to pending", key, value)
		s.pendingStorage[key] = value
		if value != s.originStorage[key] {
			slotsToPrefetch = append(slotsToPrefetch, common.CopyBytes(key[:])) // Copy needed for closure
		}
	}
	if s.db.prefetcher != nil && prefetch && len(slotsToPrefetch) > 0 && s.data.Root != emptyRoot {
		s.db.prefetcher.prefetch(s.data.Root, slotsToPrefetch)
	}
	if len(s.dirtyStorage) > 0 {
		s.dirtyStorage = make(Storage)
	}

	// stage1-substate: clear stateObject.ResearchTouched
	s.ResearchTouched = make(map[common.Hash]struct{})
	s.ResearchTouchedWrite = make(map[common.Hash]struct{})
}

// updateTrie writes cached storage modifications into the object's storage trie.
// It will return nil if the trie has not been loaded and no changes have been made
func (s *stateObject) updateTrie(db Database) Trie {
	// fmt.Println("      UpdateTrie", s.txHash, s.address, s.data.Balance)
	// Make sure all dirty slots are finalized into the pending storage area
	s.finalise(false) // Don't prefetch anymore, pull directly if need be
	if len(s.pendingStorage) == 0 {
		return s.trie
	}
	// Track the amount of time wasted on updating the storage trie
	if metrics.EnabledExpensive {
		defer func(start time.Time) { s.db.StorageUpdates += time.Since(start) }(time.Now())
	}
	// The snapshot storage map for the object
	var storage map[common.Hash][]byte
	// Insert all the pending updates into the trie
	tr := s.getTrie(db)
	hasher := s.db.hasher

	usedStorage := make([][]byte, 0, len(s.pendingStorage))
	for key, value := range s.pendingStorage {
		// Skip noop changes, persist actual changes
		// fmt.Println("       for key value in range s.pendingStorage", key, value)
		if value == s.originStorage[key] {
			continue
		}
		s.originStorage[key] = value

		var v []byte
		if (value == common.Hash{}) {
			s.setError(tr.TryDelete(key[:]))
			// fmt.Println(" DELETE")
			if s.txHash != common.HexToHash("0x0") {
				// fmt.Println("     ", common.GlobalBlockNumber, s.txHash)
				// fmt.Println("     MyTryUpdate", s.address, key, common.Bytes2Hex(v))
				// fmt.Println("     MyTryUpdate", common.TxWriteList[common.GlobalTxHash])
				// fmt.Println("     MyTryUpdate", common.TxWriteList[common.GlobalTxHash][s.address])
				// fmt.Println("     MyTryUpdate", common.TxWriteList[common.GlobalTxHash][s.address].Storage)
				// fmt.Println("     MyTryUpdate", common.TxWriteList[common.GlobalTxHash][s.address].Storage[key])
				// common.TxWriteList[s.txHash][s.address].Storage[key] = value
				for txhash, storage := range s.TxStorage {
					if storage[key] == value {
						// fmt.Println("WHy1", txhash, s.address, key, value)
						if _, exist := common.TxWriteList[txhash][s.address]; exist {
							common.TxWriteList[txhash][s.address].Storage[key] = value

						}
					}
				}
			} else {
				fmt.Println("delete hardfork obj ", common.GlobalTxHash, key)
				common.HardFork[s.address].Storage[key] = value
			}

			s.db.StorageDeleted += 1
		} else {
			// Encoding []byte cannot fail, ok to ignore the error.
			v, _ = rlp.EncodeToBytes(common.TrimLeftZeroes(value[:]))
			// fmt.Println(" UPDATE")
			if s.txHash != common.HexToHash("0x0") {
				// if s.address == common.HexToAddress("0x3dBDc81a6edc94c720B0B88FB65dBD7e395fDcf6") {
				// 	fmt.Println("check ", s.address, s.txHash, key, value)
				// }
				// // fmt.Println("     ", common.GlobalBlockNumber, s.txHash, common.GlobalTxHash)
				// fmt.Println("     MyTryUpdate", s.txHash, key, common.Bytes2Hex(v))
				// fmt.Println("     MyTryUpdate", common.TxWriteList[s.txHash])
				// fmt.Println("     MyTryUpdate", common.TxWriteList[s.txHash][s.address])
				// fmt.Println("     MyTryUpdate", common.TxWriteList[s.txHash][s.address].Storage)
				// fmt.Println("     MyTryUpdate", common.TxWriteList[s.txHash][s.address].Storage[key])
				for txhash, storage := range s.TxStorage {
					if storage[key] == value {
						// fmt.Println()
						// fmt.Println("WHy2", txhash, s.address, key, value)

						if _, exist := common.TxWriteList[txhash][s.address]; exist {
							// fmt.Println("    WHy2")
							common.TxWriteList[txhash][s.address].Storage[key] = value
							sa := common.TxWriteList[txhash][s.address]
							if s.address == common.HexToAddress("0xbaa54d6e90c3f4d7ebec11bd180134c7ed8ebb52") {
								fmt.Println("Fy.", s.txHash, txhash, s.address, sa.StorageRoot, sa.Storage)
								fmt.Println("  insertTrie", key, value, common.TxWriteList[txhash][s.address].Storage[key])
							}
							// fmt.Println("    Fuckyou", sa.Balance, sa.StorageRoot, sa.Storage, common.TxWriteList[txhash][s.address].Storage[key])

						}

					}
				}

			} else {
				fmt.Println("update hardfork obj ", common.GlobalTxHash, key)
				common.HardFork[s.address].Storage[key] = value
			}

			s.setError(tr.MyTryUpdate(key[:], v, s.txHash, s.address)) //jhkim
			// fmt.Println(" after updatetrie/address", s.address, " storageroot", tr.Hash())

			s.db.StorageUpdated += 1
		}
		// If state snapshotting is active, cache the data til commit
		if s.db.snap != nil {
			if storage == nil {
				// Retrieve the old storage map, if available, create a new one otherwise
				if storage = s.db.snapStorage[s.addrHash]; storage == nil {
					storage = make(map[common.Hash][]byte)
					s.db.snapStorage[s.addrHash] = storage
				}
			}
			storage[crypto.HashData(hasher, key[:])] = v // v will be nil if it's deleted
		}
		usedStorage = append(usedStorage, common.CopyBytes(key[:])) // Copy needed for closure
	}
	if s.db.prefetcher != nil {
		s.db.prefetcher.used(s.data.Root, usedStorage)
	}
	if len(s.pendingStorage) > 0 {
		s.pendingStorage = make(Storage)
	}
	if s.address == common.HexToAddress("0xbaa54d6e90c3f4d7ebec11bd180134c7ed8ebb52") {
		fmt.Println("txhash", s.txHash)
		fmt.Println("address", s.address)
		fmt.Println("substatealloc", common.TxWriteList[s.txHash])
		fmt.Println("substate account", common.TxWriteList[s.txHash][s.address])
		fmt.Println("s.balance", s.Balance())
		fmt.Println("s.nonce", s.Nonce())

		fmt.Println("s.dirty Storage", s.dirtyStorage)
	}
	// fmt.Println("common.TxWriteList[s.txHash]", common.TxWriteList[s.txHash])
	if _, exist := common.TxWriteList[s.txHash][s.address]; exist {
		common.TxWriteList[s.txHash][s.address].StorageRoot = tr.Hash()
	}
	// fmt.Println("plz", s.txHash, s.address, common.TxWriteList[s.txHash][s.address].Storage)
	return tr
}

// UpdateRoot sets the trie root to the current root hash of
func (s *stateObject) updateRoot(db Database) {
	// If nothing changed, don't bother with hashing anything
	// fmt.Println("    updateRoot")
	if s.updateTrie(db) == nil {
		return
	}
	// Track the amount of time wasted on hashing the storage trie
	if metrics.EnabledExpensive {
		defer func(start time.Time) { s.db.StorageHashes += time.Since(start) }(time.Now())
	}
	s.data.Root = s.trie.Hash()
}

// CommitTrie the storage trie of the object to db.
// This updates the trie root.
func (s *stateObject) CommitTrie(db Database) (int, error) {
	// fmt.Println("    CommitTrie", s.txHash, s.address)
	// If nothing changed, don't bother with hashing anything
	if s.updateTrie(db) == nil {
		return 0, nil
	}
	if s.dbErr != nil {
		return 0, s.dbErr
	}
	// Track the amount of time wasted on committing the storage trie
	if metrics.EnabledExpensive {
		defer func(start time.Time) { s.db.StorageCommits += time.Since(start) }(time.Now())
	}
	// jhkim: commit storage trie
	root, committed, err := s.trie.Commit(nil)
	// if common.GlobalBlockNumber == 432361 || common.GlobalBlockNumber == 432368 {
	// 	fmt.Println("  Print Storage Trie", s.trie.Hash())
	// 	s.trie.Print()

	// 	fmt.Println()
	// }
	if err == nil {
		s.data.Root = root
	}
	return committed, err
}

// AddBalance adds amount to s's balance.
// It is used to add funds to the destination account of a transfer.
func (s *stateObject) AddBalance(amount *big.Int) {
	// EIP161: We must check emptiness for the objects such that the account
	// clearing (0,0,0 objects) can take effect.
	if amount.Sign() == 0 {
		if s.empty() {
			s.touch()
		}
		return
	}
	s.SetBalance(new(big.Int).Add(s.Balance(), amount))
}

// SubBalance removes amount from s's balance.
// It is used to remove funds from the origin account of a transfer.
func (s *stateObject) SubBalance(amount *big.Int) {
	if amount.Sign() == 0 {
		return
	}
	s.SetBalance(new(big.Int).Sub(s.Balance(), amount))
}

func (s *stateObject) SetBalance(amount *big.Int) {
	s.db.journal.append(balanceChange{
		account: &s.address,
		prev:    new(big.Int).Set(s.data.Balance),
	})
	s.setBalance(amount)
}

func (s *stateObject) setBalance(amount *big.Int) {
	s.data.Balance = amount
}

func (s *stateObject) deepCopy(db *StateDB) *stateObject {
	stateObject := newObject(db, s.address, s.data)
	if s.trie != nil {
		stateObject.trie = db.db.CopyTrie(s.trie)
	}
	stateObject.code = s.code
	stateObject.dirtyStorage = s.dirtyStorage.Copy()
	stateObject.originStorage = s.originStorage.Copy()
	stateObject.pendingStorage = s.pendingStorage.Copy()
	stateObject.suicided = s.suicided
	stateObject.dirtyCode = s.dirtyCode
	stateObject.deleted = s.deleted

	stateObject.txHash = s.txHash // jhkim
	// stage1-substate: deepCopy stateObject.ResearchTouched
	stateObject.ResearchTouched = make(map[common.Hash]struct{})
	for key := range s.ResearchTouched {
		stateObject.ResearchTouched[key] = struct{}{}
	}

	stateObject.ResearchTouchedWrite = make(map[common.Hash]struct{})
	for key := range s.ResearchTouchedWrite {
		stateObject.ResearchTouchedWrite[key] = struct{}{}
	}
	return stateObject
}

//
// Attribute accessors
//

// Returns the address of the contract/account
func (s *stateObject) Address() common.Address {
	return s.address
}

// Code returns the contract code associated with this object, if any.
func (s *stateObject) Code(db Database) []byte {
	if s.code != nil {
		return s.code
	}
	if bytes.Equal(s.CodeHash(), emptyCodeHash) {
		return nil
	}
	code, err := db.ContractCode(s.addrHash, common.BytesToHash(s.CodeHash()))
	if err != nil {
		s.setError(fmt.Errorf("can't load code hash %x: %v", s.CodeHash(), err))
	}
	s.code = code
	return code
}

// CodeSize returns the size of the contract code associated with this object,
// or zero if none. This method is an almost mirror of Code, but uses a cache
// inside the database to avoid loading codes seen recently.
func (s *stateObject) CodeSize(db Database) int {
	if s.code != nil {
		return len(s.code)
	}
	if bytes.Equal(s.CodeHash(), emptyCodeHash) {
		return 0
	}
	size, err := db.ContractCodeSize(s.addrHash, common.BytesToHash(s.CodeHash()))
	if err != nil {
		s.setError(fmt.Errorf("can't load code size %x: %v", s.CodeHash(), err))
	}
	return size
}

func (s *stateObject) SetCode(codeHash common.Hash, code []byte) {
	prevcode := s.Code(s.db.db)
	s.db.journal.append(codeChange{
		account:  &s.address,
		prevhash: s.CodeHash(),
		prevcode: prevcode,
	})
	s.setCode(codeHash, code)
}

func (s *stateObject) setCode(codeHash common.Hash, code []byte) {
	s.code = code
	s.data.CodeHash = codeHash[:]
	s.dirtyCode = true
}

func (s *stateObject) SetNonce(nonce uint64) {
	s.db.journal.append(nonceChange{
		account: &s.address,
		prev:    s.data.Nonce,
	})
	s.setNonce(nonce)
}

func (s *stateObject) setNonce(nonce uint64) {
	s.data.Nonce = nonce
}

func (s *stateObject) CodeHash() []byte {
	return s.data.CodeHash
}

func (s *stateObject) Balance() *big.Int {
	return s.data.Balance
}

func (s *stateObject) Nonce() uint64 {
	return s.data.Nonce
}

func (s *stateObject) StorageRoot() common.Hash {
	return s.data.Root
}

// Never called, but must be present to allow stateObject to be used
// as a vm.Account interface that also satisfies the vm.ContractRef
// interface. Interfaces are awesome.
func (s *stateObject) Value() *big.Int {
	panic("Value on stateObject should never be called")
}
