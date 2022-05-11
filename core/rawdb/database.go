// Copyright 2018 The go-ethereum Authors
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
	"bytes"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/leveldb"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/olekukonko/tablewriter"
)

// freezerdb is a database wrapper that enabled freezer data retrievals.
type freezerdb struct {
	ethdb.KeyValueStore
	ethdb.AncientStore
}

// Close implements io.Closer, closing both the fast key-value store as well as
// the slow ancient tables.
func (frdb *freezerdb) Close() error {
	var errs []error
	if err := frdb.AncientStore.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := frdb.KeyValueStore.Close(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) != 0 {
		return fmt.Errorf("%v", errs)
	}
	return nil
}

// Freeze is a helper method used for external testing to trigger and block until
// a freeze cycle completes, without having to sleep for a minute to trigger the
// automatic background run.
func (frdb *freezerdb) Freeze(threshold uint64) error {
	if frdb.AncientStore.(*freezer).readonly {
		return errReadOnly
	}
	// Set the freezer threshold to a temporary value
	defer func(old uint64) {
		atomic.StoreUint64(&frdb.AncientStore.(*freezer).threshold, old)
	}(atomic.LoadUint64(&frdb.AncientStore.(*freezer).threshold))
	atomic.StoreUint64(&frdb.AncientStore.(*freezer).threshold, threshold)

	// Trigger a freeze cycle and block until it's done
	trigger := make(chan struct{}, 1)
	frdb.AncientStore.(*freezer).trigger <- trigger
	<-trigger
	return nil
}

// nofreezedb is a database wrapper that disables freezer data retrievals.
type nofreezedb struct {
	ethdb.KeyValueStore
}

// HasAncient returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) HasAncient(kind string, number uint64) (bool, error) {
	return false, errNotSupported
}

// Ancient returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) Ancient(kind string, number uint64) ([]byte, error) {
	return nil, errNotSupported
}

// AncientRange returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) AncientRange(kind string, start, max, maxByteSize uint64) ([][]byte, error) {
	return nil, errNotSupported
}

// Ancients returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) Ancients() (uint64, error) {
	return 0, errNotSupported
}

// AncientSize returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) AncientSize(kind string) (uint64, error) {
	return 0, errNotSupported
}

// ModifyAncients is not supported.
func (db *nofreezedb) ModifyAncients(func(ethdb.AncientWriteOp) error) (int64, error) {
	return 0, errNotSupported
}

// TruncateAncients returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) TruncateAncients(items uint64) error {
	return errNotSupported
}

// Sync returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) Sync() error {
	return errNotSupported
}

func (db *nofreezedb) ReadAncients(fn func(reader ethdb.AncientReader) error) (err error) {
	// Unlike other ancient-related methods, this method does not return
	// errNotSupported when invoked.
	// The reason for this is that the caller might want to do several things:
	// 1. Check if something is in freezer,
	// 2. If not, check leveldb.
	//
	// This will work, since the ancient-checks inside 'fn' will return errors,
	// and the leveldb work will continue.
	//
	// If we instead were to return errNotSupported here, then the caller would
	// have to explicitly check for that, having an extra clause to do the
	// non-ancient operations.
	return fn(db)
}

// NewDatabase creates a high level database on top of a given key-value data
// store without a freezer moving immutable chain segments into cold storage.
func NewDatabase(db ethdb.KeyValueStore) ethdb.Database {
	return &nofreezedb{KeyValueStore: db}
}

// NewDatabaseWithFreezer creates a high level database on top of a given key-
// value data store with a freezer moving immutable chain segments into cold
// storage.
func NewDatabaseWithFreezer(db ethdb.KeyValueStore, freezer string, namespace string, readonly bool) (ethdb.Database, error) {
	// Create the idle freezer instance
	frdb, err := newFreezer(freezer, namespace, readonly, freezerTableSize, FreezerNoSnappy)
	if err != nil {
		return nil, err
	}
	// Since the freezer can be stored separately from the user's key-value database,
	// there's a fairly high probability that the user requests invalid combinations
	// of the freezer and database. Ensure that we don't shoot ourselves in the foot
	// by serving up conflicting data, leading to both datastores getting corrupted.
	//
	//   - If both the freezer and key-value store is empty (no genesis), we just
	//     initialized a new empty freezer, so everything's fine.
	//   - If the key-value store is empty, but the freezer is not, we need to make
	//     sure the user's genesis matches the freezer. That will be checked in the
	//     blockchain, since we don't have the genesis block here (nor should we at
	//     this point care, the key-value/freezer combo is valid).
	//   - If neither the key-value store nor the freezer is empty, cross validate
	//     the genesis hashes to make sure they are compatible. If they are, also
	//     ensure that there's no gap between the freezer and sunsequently leveldb.
	//   - If the key-value store is not empty, but the freezer is we might just be
	//     upgrading to the freezer release, or we might have had a small chain and
	//     not frozen anything yet. Ensure that no blocks are missing yet from the
	//     key-value store, since that would mean we already had an old freezer.

	// If the genesis hash is empty, we have a new key-value store, so nothing to
	// validate in this method. If, however, the genesis hash is not nil, compare
	// it to the freezer content.
	if kvgenesis, _ := db.Get(headerHashKey(0)); len(kvgenesis) > 0 {
		if frozen, _ := frdb.Ancients(); frozen > 0 {
			// If the freezer already contains something, ensure that the genesis blocks
			// match, otherwise we might mix up freezers across chains and destroy both
			// the freezer and the key-value store.
			frgenesis, err := frdb.Ancient(freezerHashTable, 0)
			if err != nil {
				return nil, fmt.Errorf("failed to retrieve genesis from ancient %v", err)
			} else if !bytes.Equal(kvgenesis, frgenesis) {
				return nil, fmt.Errorf("genesis mismatch: %#x (leveldb) != %#x (ancients)", kvgenesis, frgenesis)
			}
			// Key-value store and freezer belong to the same network. Ensure that they
			// are contiguous, otherwise we might end up with a non-functional freezer.
			if kvhash, _ := db.Get(headerHashKey(frozen)); len(kvhash) == 0 {
				// Subsequent header after the freezer limit is missing from the database.
				// Reject startup is the database has a more recent head.
				if *ReadHeaderNumber(db, ReadHeadHeaderHash(db)) > frozen-1 {
					return nil, fmt.Errorf("gap (#%d) in the chain between ancients and leveldb", frozen)
				}
				// Database contains only older data than the freezer, this happens if the
				// state was wiped and reinited from an existing freezer.
			}
			// Otherwise, key-value store continues where the freezer left off, all is fine.
			// We might have duplicate blocks (crash after freezer write but before key-value
			// store deletion, but that's fine).
		} else {
			// If the freezer is empty, ensure nothing was moved yet from the key-value
			// store, otherwise we'll end up missing data. We check block #1 to decide
			// if we froze anything previously or not, but do take care of databases with
			// only the genesis block.
			if ReadHeadHeaderHash(db) != common.BytesToHash(kvgenesis) {
				// Key-value store contains more data than the genesis block, make sure we
				// didn't freeze anything yet.
				if kvblob, _ := db.Get(headerHashKey(1)); len(kvblob) == 0 {
					return nil, errors.New("ancient chain segments already extracted, please set --datadir.ancient to the correct path")
				}
				// Block #1 is still in the database, we're allowed to init a new feezer
			}
			// Otherwise, the head header is still the genesis, we're allowed to init a new
			// feezer.
		}
	}
	// Freezer is consistent with the key-value database, permit combining the two
	if !frdb.readonly {
		frdb.wg.Add(1)
		go func() {
			frdb.freeze(db)
			frdb.wg.Done()
		}()
	}
	return &freezerdb{
		KeyValueStore: db,
		AncientStore:  frdb,
	}, nil
}

// NewMemoryDatabase creates an ephemeral in-memory key-value database without a
// freezer moving immutable chain segments into cold storage.
func NewMemoryDatabase() ethdb.Database {
	return NewDatabase(memorydb.New())
}

// NewMemoryDatabaseWithCap creates an ephemeral in-memory key-value database
// with an initial starting capacity, but without a freezer moving immutable
// chain segments into cold storage.
func NewMemoryDatabaseWithCap(size int) ethdb.Database {
	return NewDatabase(memorydb.NewWithCap(size))
}

// NewLevelDBDatabase creates a persistent key-value database without a freezer
// moving immutable chain segments into cold storage.
func NewLevelDBDatabase(file string, cache int, handles int, namespace string, readonly bool) (ethdb.Database, error) {
	db, err := leveldb.New(file, cache, handles, namespace, readonly)
	if err != nil {
		return nil, err
	}
	return NewDatabase(db), nil
}

// NewLevelDBDatabaseWithFreezer creates a persistent key-value database with a
// freezer moving immutable chain segments into cold storage.
func NewLevelDBDatabaseWithFreezer(file string, cache int, handles int, freezer string, namespace string, readonly bool) (ethdb.Database, error) {
	kvdb, err := leveldb.New(file, cache, handles, namespace, readonly)
	if err != nil {
		return nil, err
	}
	frdb, err := NewDatabaseWithFreezer(kvdb, freezer, namespace, readonly)
	if err != nil {
		kvdb.Close()
		return nil, err
	}
	return frdb, nil
}

type counter uint64

func (c counter) String() string {
	return fmt.Sprintf("%d", c)
}

func (c counter) Percentage(current uint64) string {
	return fmt.Sprintf("%d", current*100/uint64(c))
}

// stat stores sizes and count for a parameter
type stat struct {
	size  common.StorageSize
	count counter
}

// Add size to the stat and increase the counter by 1
func (s *stat) Add(size common.StorageSize) {
	s.size += size
	s.count++
}

func (s *stat) Size() string {
	return s.size.String()
}

func (s *stat) Count() string {
	return s.count.String()
}

// InspectDatabase traverses the entire database and checks the size
// of all different categories of data.
func InspectDatabase(db ethdb.Database, keyPrefix, keyStart []byte) error {
	it := db.NewIterator(keyPrefix, keyStart)
	defer it.Release()

	var (
		count  int64
		start  = time.Now()
		logged = time.Now()

		// Key-value store statistics
		headers         stat
		bodies          stat
		receipts        stat
		tds             stat
		numHashPairings stat
		hashNumPairings stat
		tries           stat
		codes           stat
		txLookups       stat
		accountSnaps    stat
		storageSnaps    stat
		preimages       stat
		bloomBits       stat
		cliqueSnaps     stat

		// Ancient store statistics
		ancientHeadersSize  common.StorageSize
		ancientBodiesSize   common.StorageSize
		ancientReceiptsSize common.StorageSize
		ancientTdsSize      common.StorageSize
		ancientHashesSize   common.StorageSize

		// Les statistic
		chtTrieNodes   stat
		bloomTrieNodes stat

		// Meta- and unaccounted data
		metadata    stat
		unaccounted stat

		// Totals
		total common.StorageSize
	)
	// Inspect key-value database first.
	for it.Next() {
		var (
			key  = it.Key()
			size = common.StorageSize(len(key) + len(it.Value()))
		)
		total += size
		switch {
		case bytes.HasPrefix(key, headerPrefix) && len(key) == (len(headerPrefix)+8+common.HashLength):
			headers.Add(size)
		case bytes.HasPrefix(key, blockBodyPrefix) && len(key) == (len(blockBodyPrefix)+8+common.HashLength):
			bodies.Add(size)
		case bytes.HasPrefix(key, blockReceiptsPrefix) && len(key) == (len(blockReceiptsPrefix)+8+common.HashLength):
			receipts.Add(size)
		case bytes.HasPrefix(key, headerPrefix) && bytes.HasSuffix(key, headerTDSuffix):
			tds.Add(size)
		case bytes.HasPrefix(key, headerPrefix) && bytes.HasSuffix(key, headerHashSuffix):
			numHashPairings.Add(size)
		case bytes.HasPrefix(key, headerNumberPrefix) && len(key) == (len(headerNumberPrefix)+common.HashLength):
			hashNumPairings.Add(size)
		case len(key) == common.HashLength:
			// jhkim: print dbinspect
			// if common.Bytes2Hex(key) == "14877df2d554fb72cb35a387400af0fb3fee5e11976d0a6ab4c848fb6e4021af" || //storageTrie node
			// 	common.Bytes2Hex(key) == "2f60cfb65595474f839d3108ce367ff24cc323f28076426e87110b42c8f93043" ||
			// 	common.Bytes2Hex(key) == "900e89da20be36e2bb6b09b4df1c8d48a70bbfdcfdfbfc4c40078da48fabb7d4" ||
			// 	common.Bytes2Hex(key) == "5dce1b049e5a53760dd9b592e8837c6e6e74ef29af8ffd3ec7020220b75da5d8" ||
			// 	common.Bytes2Hex(key) == "0225f9179440f82b3c2356b372cda0bf55b9e344d197e9a58b5fa698f7a38894" ||
			// 	common.Bytes2Hex(key) == "205e55fab790df8abbaa0e6417cbec8355f4d2f99afd268c06073dc352523098" ||

			// 	common.Bytes2Hex(key) == "ffe91d908b67e96d2ec329458cbcaeb6ac700cf3dbd709bda53d99d5af08c5e5" || //stateTrie node
			// 	common.Bytes2Hex(key) == "b06a56c46d261b3a3b5bc5c8a34e8ee0974502b7de3f608c589696a2d3b29b45" ||
			// 	common.Bytes2Hex(key) == "de651eba49b8e791d726b70cee8ac4fe108750b1ac7c50809538d344ceb56699" ||
			// 	common.Bytes2Hex(key) == "9c141177ca3364fc5097cfe161c5364faab7c857d6a9cf148b96587a06911768" ||
			// 	common.Bytes2Hex(key) == "47308033208c2f75a4b1321c9926b737839f44c7985efc50025d439ca7d01b57" ||
			// 	common.Bytes2Hex(key) == "5f63c98016d650edec6a8679c0f3281b54f46c50ea49786eeea8334d30fa264b" ||
			// 	common.Bytes2Hex(key) == "ea048fbc0c50dfce5a94562db9c32fb6a7d152c2fea542bdf085bdb29783832c" ||
			// 	common.Bytes2Hex(key) == "563be81458c32519ca512629926ae28de48559a363d62debdaf71620a63975ac" ||
			// 	common.Bytes2Hex(key) == "1fbcff31b81ca6b8e9fb4cd55859d0fd83ac87cfa2cd287ecb7534aadb9518d3" ||
			// 	common.Bytes2Hex(key) == "cb3e3b1b900bd10870ad6bfdea86db664cd08074e2d048cf329906209c84e170" ||
			// 	common.Bytes2Hex(key) == "746b004b12f042cfa45a33a2d056ee5152af9096fbe241a2e6b7795e3eda07cc" ||
			// 	common.Bytes2Hex(key) == "415ef63a4f3e44da71c127d4566636dde0f992c25fa6a5daa70bb832aff9d85d" ||
			// 	common.Bytes2Hex(key) == "e083b2bd27e57f2fa91cb4ce1fc3ffa29d9cf2cd4a8639307cee6d04aaf256b1" ||
			// 	common.Bytes2Hex(key) == "de9f1d840347d0ebc64310a965d17fd2cc2f783c346403337c18f1ba667400be" ||
			// 	common.Bytes2Hex(key) == "f48e528cac3a692afc8df468a45d4cdad5e1d321b462e0b8e314c48ec5b98423" ||
			// 	common.Bytes2Hex(key) == "55725087c19c5310e80760bceea8b92d74c38cdbd3ee0567e99abe83fad6c7dd" ||
			// 	common.Bytes2Hex(key) == "e7bd7c3978f11f6943ec29a31aa2194a3b4454b41ace16a1144259b96820e491" ||
			// 	common.Bytes2Hex(key) == "3502e5afe3353284bb22ea6f10da731092923eeaa62f3c414f9bdefbe669b008" {

			// 	fmt.Println("DB INSPECT", "TRIE", "key:", common.Bytes2Hex(key), "value:", common.Bytes2Hex(it.Value()))
			// }
			tries.Add(size)
		case bytes.HasPrefix(key, CodePrefix) && len(key) == len(CodePrefix)+common.HashLength:
			codes.Add(size)
		case bytes.HasPrefix(key, txLookupPrefix) && len(key) == (len(txLookupPrefix)+common.HashLength):
			txLookups.Add(size)
		case bytes.HasPrefix(key, SnapshotAccountPrefix) && len(key) == (len(SnapshotAccountPrefix)+common.HashLength):
			accountSnaps.Add(size)
		case bytes.HasPrefix(key, SnapshotStoragePrefix) && len(key) == (len(SnapshotStoragePrefix)+2*common.HashLength):
			storageSnaps.Add(size)
		case bytes.HasPrefix(key, PreimagePrefix) && len(key) == (len(PreimagePrefix)+common.HashLength):
			preimages.Add(size)
		case bytes.HasPrefix(key, configPrefix) && len(key) == (len(configPrefix)+common.HashLength):
			metadata.Add(size)
		case bytes.HasPrefix(key, bloomBitsPrefix) && len(key) == (len(bloomBitsPrefix)+10+common.HashLength):
			bloomBits.Add(size)
		case bytes.HasPrefix(key, BloomBitsIndexPrefix):
			bloomBits.Add(size)
		case bytes.HasPrefix(key, []byte("clique-")) && len(key) == 7+common.HashLength:
			cliqueSnaps.Add(size)
		case bytes.HasPrefix(key, []byte("cht-")) ||
			bytes.HasPrefix(key, []byte("chtIndexV2-")) ||
			bytes.HasPrefix(key, []byte("chtRootV2-")): // Canonical hash trie
			chtTrieNodes.Add(size)
		case bytes.HasPrefix(key, []byte("blt-")) ||
			bytes.HasPrefix(key, []byte("bltIndex-")) ||
			bytes.HasPrefix(key, []byte("bltRoot-")): // Bloomtrie sub
			bloomTrieNodes.Add(size)
		default:
			var accounted bool
			for _, meta := range [][]byte{
				databaseVersionKey, headHeaderKey, headBlockKey, headFastBlockKey, lastPivotKey,
				fastTrieProgressKey, snapshotDisabledKey, SnapshotRootKey, snapshotJournalKey,
				snapshotGeneratorKey, snapshotRecoveryKey, txIndexTailKey, fastTxLookupLimitKey,
				uncleanShutdownKey, badBlockKey, transitionStatusKey,
			} {
				if bytes.Equal(key, meta) {
					metadata.Add(size)
					accounted = true
					break
				}
			}
			if !accounted {
				unaccounted.Add(size)
			}
		}
		count++
		if count%1000 == 0 && time.Since(logged) > 8*time.Second {
			log.Info("Inspecting database", "count", count, "elapsed", common.PrettyDuration(time.Since(start)))
			logged = time.Now()
		}
	}
	// Inspect append-only file store then.
	ancientSizes := []*common.StorageSize{&ancientHeadersSize, &ancientBodiesSize, &ancientReceiptsSize, &ancientHashesSize, &ancientTdsSize}
	for i, category := range []string{freezerHeaderTable, freezerBodiesTable, freezerReceiptTable, freezerHashTable, freezerDifficultyTable} {
		if size, err := db.AncientSize(category); err == nil {
			*ancientSizes[i] += common.StorageSize(size)
			total += common.StorageSize(size)
		}
	}
	// Get number of ancient rows inside the freezer
	ancients := counter(0)
	if count, err := db.Ancients(); err == nil {
		ancients = counter(count)
	}
	// Display the database statistic.
	stats := [][]string{
		{"Key-Value store", "Headers", headers.Size(), headers.Count()},
		{"Key-Value store", "Bodies", bodies.Size(), bodies.Count()},
		{"Key-Value store", "Receipt lists", receipts.Size(), receipts.Count()},
		{"Key-Value store", "Difficulties", tds.Size(), tds.Count()},
		{"Key-Value store", "Block number->hash", numHashPairings.Size(), numHashPairings.Count()},
		{"Key-Value store", "Block hash->number", hashNumPairings.Size(), hashNumPairings.Count()},
		{"Key-Value store", "Transaction index", txLookups.Size(), txLookups.Count()},
		{"Key-Value store", "Bloombit index", bloomBits.Size(), bloomBits.Count()},
		{"Key-Value store", "Contract codes", codes.Size(), codes.Count()},
		{"Key-Value store", "Trie nodes", tries.Size(), tries.Count()},
		{"Key-Value store", "Trie preimages", preimages.Size(), preimages.Count()},
		{"Key-Value store", "Account snapshot", accountSnaps.Size(), accountSnaps.Count()},
		{"Key-Value store", "Storage snapshot", storageSnaps.Size(), storageSnaps.Count()},
		{"Key-Value store", "Clique snapshots", cliqueSnaps.Size(), cliqueSnaps.Count()},
		{"Key-Value store", "Singleton metadata", metadata.Size(), metadata.Count()},
		{"Ancient store", "Headers", ancientHeadersSize.String(), ancients.String()},
		{"Ancient store", "Bodies", ancientBodiesSize.String(), ancients.String()},
		{"Ancient store", "Receipt lists", ancientReceiptsSize.String(), ancients.String()},
		{"Ancient store", "Difficulties", ancientTdsSize.String(), ancients.String()},
		{"Ancient store", "Block number->hash", ancientHashesSize.String(), ancients.String()},
		{"Light client", "CHT trie nodes", chtTrieNodes.Size(), chtTrieNodes.Count()},
		{"Light client", "Bloom trie nodes", bloomTrieNodes.Size(), bloomTrieNodes.Count()},
	}
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Database", "Category", "Size", "Items"})
	table.SetFooter([]string{"", "Total", total.String(), " "})
	table.AppendBulk(stats)
	table.Render()

	if unaccounted.size > 0 {
		log.Error("Database contains unaccounted data", "size", unaccounted.size, "count", unaccounted.count)
	}

	return nil
}
