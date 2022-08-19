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
	"encoding/json"
	"fmt"
	"time"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

// DumpConfig is a set of options to control what portions of the statewill be
// iterated and collected.
type DumpConfig struct {
	SkipCode          bool
	SkipStorage       bool
	OnlyWithAddresses bool
	Start             []byte
	Max               uint64
}

// DumpCollector interface which the state trie calls during iteration
type DumpCollector interface {
	// OnRoot is called with the state root
	OnRoot(common.Hash)
	// OnAccount is called once for each account in the trie
	OnAccount(common.Address, DumpAccount)
	// (joonha)
	getAccount(common.Address) (DumpAccount, bool)
}

// DumpAccount represents an account in the state.
type DumpAccount struct {
	Balance   string                 `json:"balance"`
	Nonce     uint64                 `json:"nonce"`
	Root      hexutil.Bytes          `json:"root"`
	CodeHash  hexutil.Bytes          `json:"codeHash"`
	Code      hexutil.Bytes          `json:"code,omitempty"`
	Storage   map[common.Hash]string `json:"storage,omitempty"`
	Address   *common.Address        `json:"address,omitempty"` // Address only present in iterative (line-by-line) mode
	SecureKey hexutil.Bytes          `json:"key,omitempty"`     // If we don't have address, we can output the key

}

// Dump represents the full dump in a collected format, as one large map.
type Dump struct {
	Root     string                         `json:"root"`
	Accounts map[common.Address]DumpAccount `json:"accounts"`
}

// OnRoot implements DumpCollector interface
func (d *Dump) OnRoot(root common.Hash) {
	d.Root = fmt.Sprintf("%x", root)
}

// OnAccount implements DumpCollector interface
func (d *Dump) OnAccount(addr common.Address, account DumpAccount) {
	d.Accounts[addr] = account
}

// getAccount returns dumpAccount of a given address (joonha)
func (d *Dump) getAccount(addr common.Address) (DumpAccount, bool) {
	acc, doExist := d.Accounts[addr]
	return acc, doExist
}

// IteratorDump is an implementation for iterating over data.
type IteratorDump struct {
	Root     string                         `json:"root"`
	Accounts map[common.Address]DumpAccount `json:"accounts"`
	Next     []byte                         `json:"next,omitempty"` // nil if no more accounts
}

// OnRoot implements DumpCollector interface
func (d *IteratorDump) OnRoot(root common.Hash) {
	d.Root = fmt.Sprintf("%x", root)
}

// OnAccount implements DumpCollector interface
func (d *IteratorDump) OnAccount(addr common.Address, account DumpAccount) {
	d.Accounts[addr] = account
}

// getAccount returns dumpAccount of a given address (joonha)
func (d *IteratorDump) getAccount(addr common.Address) (DumpAccount, bool) {
	acc, doExist := d.Accounts[addr]
	return acc, doExist
}

// iterativeDump is a DumpCollector-implementation which dumps output line-by-line iteratively.
type iterativeDump struct {
	*json.Encoder
}

// OnAccount implements DumpCollector interface
func (d iterativeDump) OnAccount(addr common.Address, account DumpAccount) {
	dumpAccount := &DumpAccount{
		Balance:   account.Balance,
		Nonce:     account.Nonce,
		Root:      account.Root,
		CodeHash:  account.CodeHash,
		Code:      account.Code,
		Storage:   account.Storage,
		SecureKey: account.SecureKey,
		Address:   nil,
	}
	if addr != (common.Address{}) {
		dumpAccount.Address = &addr
	}
	d.Encode(dumpAccount)
}

// OnRoot implements DumpCollector interface
func (d iterativeDump) OnRoot(root common.Hash) {
	d.Encode(struct {
		Root common.Hash `json:"root"`
	}{root})
}

// getAccount returns dumpAccount of a given address (joonha)
func (d iterativeDump) getAccount(addr common.Address) (DumpAccount, bool) {
	// acc, doExist := d.Accounts[addr]
	return d.getAccount(addr)
}

// DumpToCollector iterates the state according to the given options and inserts
// the items into a collector for aggregation or serialization.
func (s *StateDB) DumpToCollector(c DumpCollector, conf *DumpConfig) (nextKey []byte) {
	// Sanitize the input to allow nil configs
	if conf == nil {
		conf = new(DumpConfig)
	}
	var (
		missingPreimages int
		accounts         uint64
		start            = time.Now()
		logged           = time.Now()
	)
	log.Info("Trie dumping started", "root", s.trie.Hash())
	c.OnRoot(s.trie.Hash())

	it := trie.NewIterator(s.trie.NodeIterator(conf.Start))
	for it.Next() {
		var data types.StateAccount
		if err := rlp.DecodeBytes(it.Value, &data); err != nil {
			panic(err)
		}
		account := DumpAccount{
			Balance:   data.Balance.String(),
			Nonce:     data.Nonce,
			Root:      data.Root[:],
			CodeHash:  data.CodeHash,
			SecureKey: it.Key,
		}
		addrBytes := s.trie.GetKey(it.Key)
		if addrBytes == nil {
			// Preimage missing
			missingPreimages++
			if conf.OnlyWithAddresses {
				continue
			}
			account.SecureKey = it.Key
		}
		addr := common.BytesToAddress(addrBytes)
		obj := newObject(s, addr, data)
		if !conf.SkipCode {
			account.Code = obj.Code(s.db)
		}
		if !conf.SkipStorage {
			account.Storage = make(map[common.Hash]string)
			storageIt := trie.NewIterator(obj.getTrie(s.db).NodeIterator(nil))
			for storageIt.Next() {
				_, content, _, err := rlp.Split(storageIt.Value)
				if err != nil {
					log.Error("Failed to decode the value returned by iterator", "error", err)
					continue
				}
				account.Storage[common.BytesToHash(s.trie.GetKey(storageIt.Key))] = common.Bytes2Hex(content)
			}
		}
		c.OnAccount(addr, account)
		accounts++
		if time.Since(logged) > 8*time.Second {
			log.Info("Trie dumping in progress", "at", it.Key, "accounts", accounts,
				"elapsed", common.PrettyDuration(time.Since(start)))
			logged = time.Now()
		}
		if conf.Max > 0 && accounts >= conf.Max {
			if it.Next() {
				nextKey = it.Key
			}
			break
		}
	}
	if missingPreimages > 0 {
		log.Warn("Dump incomplete due to missing preimages", "missing", missingPreimages)
	}
	log.Info("Trie dumping complete", "accounts", accounts,
		"elapsed", common.PrettyDuration(time.Since(start)))

	return nextKey
}

/*
* DumpToCollector_Ethane iterates the state according to the given options and inserts
* the items into a collector for aggregation or serialization.
* Different from original DumpToCollector, DumpToCollector_Ethane merges crumb accounts.
* Only for this dump test, crumb's nonce should be set to zero not to blockNum * 64.
* (commenter: joonha)
*/
func (s *StateDB) DumpToCollector_Ethane(c DumpCollector, conf *DumpConfig) (nextKey []byte) {
	// Sanitize the input to allow nil configs
	if conf == nil {
		conf = new(DumpConfig)
	}
	var (
		missingPreimages int
		accounts         uint64
		start            = time.Now()
		// logged           = time.Now()
	)
	// map을 sorted order로 iterate하려면 key를 sort()한 후에 iterate해야 한다고 함.
	// https://www.educative.io/answers/how-to-iterate-over-a-golang-map-in-sorted-order

	// 그 최종값은 dump의 root로 할 필요는 없을 것 같음.
	// dump의 account를 sorting해서 json 파일에 적는다.
	// 그런 다음에 그냥 그 json 파일 두 개가 일치하는지를 살피면 됨.


	it := trie.NewIterator(s.trie.NodeIterator(conf.Start))
	for it.Next() {
		var data types.StateAccount
		if err := rlp.DecodeBytes(it.Value, &data); err != nil {
			panic(err)
		}
		account := DumpAccount{ // incoming account
			Balance:   data.Balance.String(),
			Nonce:     data.Nonce,
			Root:      data.Root[:],
			CodeHash:  data.CodeHash,
			// SecureKey: it.Key,
		}
	
		// merge crumbs
		addr := data.Addr
		prevDumpAccount, doExist := c.getAccount(addr)
		if doExist {
			prevBalance := new(big.Int)
			prevBalance, _ = prevBalance.SetString(prevDumpAccount.Balance, 10)
			
			// var n int
			// n, _ = strconv.Atoi(prevDumpAccount.Balance) // string to int
			// prevBalance = big.NewInt(int64(n)) // int to int64 to big
			prevBalance_1 := new(big.Int)
			account.Balance = prevBalance_1.Add(data.Balance, prevBalance).String()
			// // debugging
			// if addr == common.HexToAddress("0x0000000000000000000000000000000000000000") {
			// 	fmt.Println("\n\nprevDumpAccount.Balance: ", prevDumpAccount.Balance)
			// 	// fmt.Println("n: ", n)
			// 	fmt.Println("prevBalance: ", prevBalance)
			// 	fmt.Println("data.Balance: ", data.Balance)
			// 	fmt.Println("account.Balance:, ", account.Balance)
			// }
			account.Nonce += prevDumpAccount.Nonce
			if common.BytesToHash(prevDumpAccount.Root) != common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421") { // CA's valid root
				account.Root = prevDumpAccount.Root
			}
			if common.BytesToHash(prevDumpAccount.CodeHash) != common.HexToHash("c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470") { // CA's valid codeHash
				account.CodeHash = prevDumpAccount.CodeHash
			}
		}

		// write dump
		c.OnAccount(addr, account)
		accounts++
		// if time.Since(logged) > 8*time.Second {
		// 	log.Info("Trie dumping in progress", "at", it.Key, "accounts", accounts,
		// 		"elapsed", common.PrettyDuration(time.Since(start)))
		// 	logged = time.Now()
		// }
		if conf.Max > 0 && accounts >= conf.Max {
			if it.Next() {
				nextKey = it.Key
			}
			break
		}
	}
	if missingPreimages > 0 {
		log.Warn("Dump incomplete due to missing preimages", "missing", missingPreimages)
	}
	log.Info("Trie dumping complete", "accounts", accounts,
		"elapsed", common.PrettyDuration(time.Since(start)))

	return nextKey
}

/*
* DumpToCollector_bySnapshot_Ethane iterates the snapshot and inserts
* the items into a collector for aggregation or serialization.
* Different from original DumpToCollector, DumpToCollector_bySnapshot_Ethane merges crumb accounts.
* Only for this dump test, crumb's nonce should be set to zero not to blockNum * 64.
* (commenter: joonha)
*/
func (s *StateDB) DumpToCollector_bySnapshot_Ethane(c DumpCollector, conf *DumpConfig) {
	// Sanitize the input to allow nil configs
	if conf == nil {
		conf = new(DumpConfig)
	}

	var nilHash common.Hash
	emptyRoot := common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
	emptyCode := common.HexToHash("c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470")

	// active accounts
	snapRoot := s.snap.Root() // un-updated current block root
	accountList := s.snaps.AccountList_ethane(snapRoot) // get acc's key which is 'hash(addr)'
	
	// fmt.Println("\n\naccountList:\n", accountList)
	
	// remove duplicates
	alreadyInList := make(map[common.Hash]bool)
	var accountList_no_redundancy []common.Hash
	for _, accKey := range accountList {
		if _, doExist := alreadyInList[accKey]; !doExist { // no duplicate
            alreadyInList[accKey] = true
            accountList_no_redundancy = append(accountList_no_redundancy, accKey)
        }
	}

	// fmt.Println("\n\naccountList_no_redundancy:\n", accountList_no_redundancy)
	

	for _, accKey := range accountList_no_redundancy {
		acc := s.snaps.GetAccountFromSnapshots(accKey, snapRoot, false)

		if acc == nil {
			continue // 0x0000000000000000000000000000000000000000000000000000000000000000
		}
		
		// fmt.Println("acc: ", acc)
		// fmt.Println("acc.Balance: ", acc.Balance)
		// fmt.Println("acc.Nonce: ", acc.Nonce)
		// fmt.Println("acc.Root: ", acc.Root)
		// fmt.Println("acc.CodeHash: ", acc.CodeHash)
		
		account := DumpAccount{ // incoming account
			Balance:   acc.Balance.String(),
			Nonce:     acc.Nonce,
			Root:      acc.Root[:], // when no root, it is not an emptyRoot but nil (just in case of snapshot)
			CodeHash:  acc.CodeHash, // when no code, it is not an emptyCode but nil (just in case of snapshot)
		}

		addr := acc.Addr
		if common.BytesToHash(account.Root) == nilHash {
			account.Root = emptyRoot[:]
		}
		if common.BytesToHash(account.CodeHash) == nilHash {
			account.CodeHash = emptyCode[:]
		}

		// write dump
		c.OnAccount(addr, account)
	}

	// inactive accounts
	snapRoot = s.snap_inactive.Root()
	accountList = s.snaps_inactive.AccountList_ethane(snapRoot) // get acc's key which is 'counter'

	// remove duplicates
	alreadyInList = make(map[common.Hash]bool)
	var accountList_inactive_no_redundancy []common.Hash
	for _, accKey := range accountList {
		if _, doExist := alreadyInList[accKey]; !doExist { // no duplicate
            alreadyInList[accKey] = true
            accountList_inactive_no_redundancy = append(accountList_inactive_no_redundancy, accKey)
        }
	}

	for _, accKey := range accountList_inactive_no_redundancy {
		acc := s.snaps_inactive.GetAccountFromSnapshots(accKey, snapRoot, false)
		account := DumpAccount{ // incoming account
			Balance:   acc.Balance.String(),
			Nonce:     acc.Nonce,
			Root:      acc.Root[:],
			CodeHash:  acc.CodeHash,
		}

		// fmt.Println("acc: ", acc)
		// fmt.Println("acc.Balance: ", acc.Balance)
		// fmt.Println("acc.Nonce: ", acc.Nonce)
		// fmt.Println("acc.Root: ", acc.Root)
		// fmt.Println("acc.CodeHash: ", acc.CodeHash)

		// merge inactive crumbs
		addr := acc.Addr
		prevDumpAccount, doExist := c.getAccount(addr)
		if doExist {
			prevBalance := new(big.Int)
			prevBalance, _ = prevBalance.SetString(prevDumpAccount.Balance, 10)
			prevBalance_1 := new(big.Int)
			account.Balance = prevBalance_1.Add(acc.Balance, prevBalance).String()
			// // debugging
			// if addr == common.HexToAddress("0xa1e4380a3b1f749673e270229993ee55f35663b4") { // 46147
			// 	fmt.Println("\n\nprevDumpAccount.Balance: ", prevDumpAccount.Balance)
			// 	// fmt.Println("n: ", n)
			// 	fmt.Println("prevBalance: ", prevBalance)
			// 	fmt.Println("acc.Balance: ", acc.Balance)
			// 	fmt.Println("account.Balance:, ", account.Balance)
			// }
			account.Nonce += prevDumpAccount.Nonce
			if common.BytesToHash(prevDumpAccount.Root) != nilHash && common.BytesToHash(prevDumpAccount.Root) != emptyRoot { // not empty
				account.Root = prevDumpAccount.Root
			} else { // empty
				account.Root = emptyRoot[:]
			}
			if common.BytesToHash(prevDumpAccount.CodeHash) != nilHash && common.BytesToHash(prevDumpAccount.CodeHash) != emptyCode { // not empty
				account.CodeHash = prevDumpAccount.CodeHash
			} else { // empty
				account.CodeHash = emptyCode[:]
			}
		}
		if common.BytesToHash(account.Root) == nilHash {
			account.Root = emptyRoot[:]
		}
		if common.BytesToHash(account.CodeHash) == nilHash {
			account.CodeHash = emptyCode[:]
		}
		
		// write dump
		c.OnAccount(addr, account)
	}
}

// RawDump returns the entire state an a single large object
func (s *StateDB) RawDump(opts *DumpConfig) Dump {
	dump := &Dump{
		Accounts: make(map[common.Address]DumpAccount),
	}
	s.DumpToCollector(dump, opts)
	return *dump
}

// RawDump returns the entire state an a single large object
func (s *StateDB) RawDump_Ethane(opts *DumpConfig) Dump {
	dump := &Dump{
		Accounts: make(map[common.Address]DumpAccount),
	}
	s.DumpToCollector_Ethane(dump, opts)
	return *dump
}

// RawDump returns the entire state an a single large object
func (s *StateDB) RawDump_bySnapshot_Ethane(opts *DumpConfig) Dump {
	dump := &Dump{
		Accounts: make(map[common.Address]DumpAccount),
	}
	s.DumpToCollector_bySnapshot_Ethane(dump, opts)
	return *dump
}

// Dump returns a JSON string representing the entire state as a single json-object // let's use this func to export Accounts mapping (joonha)
func (s *StateDB) Dump(opts *DumpConfig) []byte {
	dump := s.RawDump(opts)
	json, err := json.MarshalIndent(dump, "", "    ")
	if err != nil {
		fmt.Println("Dump err", err)
	}
	return json
}

// Dump returns a JSON string representing the entire state as a single json-object // let's use this func to export Accounts mapping (joonha)
func (s *StateDB) Dump_Ethane(opts *DumpConfig) []byte {
	dump := s.RawDump_Ethane(opts)
	json, err := json.MarshalIndent(dump, "", "    ")
	if err != nil {
		fmt.Println("Dump err", err)
	}
	return json
}

// Dump returns a JSON string representing the entire state as a single json-object // let's use this func to export Accounts mapping (joonha)
func (s *StateDB) Dump_bySnapshot_Ethane(opts *DumpConfig) []byte {
	dump := s.RawDump_bySnapshot_Ethane(opts)
	json, err := json.MarshalIndent(dump, "", "    ")
	if err != nil {
		fmt.Println("Dump err", err)
	}
	return json
}

// IterativeDump dumps out accounts as json-objects, delimited by linebreaks on stdout
func (s *StateDB) IterativeDump(opts *DumpConfig, output *json.Encoder) {
	s.DumpToCollector(iterativeDump{output}, opts)
}

// IteratorDump dumps out a batch of accounts starts with the given start key
func (s *StateDB) IteratorDump(opts *DumpConfig) IteratorDump {
	iterator := &IteratorDump{
		Accounts: make(map[common.Address]DumpAccount),
	}
	iterator.Next = s.DumpToCollector(iterator, opts)
	return *iterator
}
