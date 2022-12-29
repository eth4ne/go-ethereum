package core

// jhkim: file for print txSubstate.txt

import (
	"fmt"
	"math/big"
	"os"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

func PrintTxSubstate(blocknumber, distance int, chainconfig params.ChainConfig) {
	ss := fmt.Sprintf("Write TxSubstate %d-%d in txt file", blocknumber-distance, blocknumber)
	filepath := common.Path + "TxSubstate" + fmt.Sprintf("%08d", blocknumber-distance+1) + "-" + fmt.Sprintf("%08d", blocknumber) + ".txt"

	f, err := os.Create(filepath)
	if err != nil {
		fmt.Printf("Cannot create result file.\n")
		os.Exit(1)
	}
	defer f.Close()

	start := time.Now()
	txcounter := 0

	for i := blocknumber - distance + 1; i <= blocknumber; i++ {
		// s := fmt.Sprintln("##########################################################################")
		if i%(distance/5) == 0 {
			elapsed := time.Since(start)
			fmt.Println("Writing file.. Block #:", i, "elapsed", elapsed.Seconds(), "s")
		}

		// s := fmt.Sprintf("\n/Block:%v\n", i)
		fmt.Fprintf(f, "\n/Block:%v\n", i)

		// add hardfork
		// if big.NewInt(int64(i)).Cmp(params.MainnetChainConfig.DAOForkBlock) == 0 {
		// 	DAOpath := common.Path + "DAOHardFork.txt"
		// 	if fdao, err := os.Open(DAOpath); err == nil {
		// 		defer f.Close()
		// 		fmt.Println("Append DAO HardFork state transition to", filepath)
		// 		scanner := bufio.NewScanner(fdao)
		// 		for scanner.Scan() {
		// 			fmt.Fprintln(f, scanner.Text())
		// 		}

		// 		fmt.Println("Done! Delete", DAOpath)
		// 		os.Remove(DAOpath)
		// 	}
		// }
		// if common.GlobalBlockNumber == 192000 {
		// 	fmt.Println("write block 1920000")
		// 	fmt.Println(big.NewInt(int64(i)))
		// 	fmt.Println(chainconfig.DAOForkBlock)
		// 	fmt.Println(chainconfig.IsHardForkBlock(big.NewInt(int64(i))))
		// 	fmt.Println(chainconfig.DAOForkBlock.Cmp(big.NewInt(int64(i))))
		// }
		if chainconfig.IsHardForkBlock(big.NewInt(int64(i))) {
			fmt.Println(i, "is hard fork block")
			fmt.Fprintln(f, "*HardFork")
			fmt.Fprintln(f, "@ReadList")
			for addr := range common.HardFork {
				fmt.Fprintf(f, ".address:%v\n", addr)
			}
			// fmt.Fprintln(f, ".address:", params.DAORefundContract)
			// for _, addr := range params.DAODrainList() {
			// 	fmt.Fprintln(f, ".address:", addr)
			// }

			// Move every DAO account and extra-balance account funds into the refund contract
			fmt.Fprintln(f, "#WriteList")
			for addr, sa := range common.HardFork {

				// jhkim: write DAO Hardfork substate for txSubstate
				// DAO drainlist
				fmt.Fprintf(f, ".address:%v\n", addr)
				fmt.Fprintf(f, "Nonce:%v\n", sa.Nonce)
				fmt.Fprintf(f, "Balance:%v\n", sa.Balance)
				// fmt.Fprintf(f, "CodeHash:%v\n", common.Bytes2Hex(sa.CodeHash))
				fmt.Fprintf(f, "CodeHash:%v\n", sa.CodeHash)
				fmt.Fprintf(f, "StorageRoot:%v\n", sa.StorageRoot)
				if sa.StorageRoot != types.EmptyRootHash {
					fmt.Fprintln(f, "Storage:")
					for key, value := range sa.Storage {
						fmt.Fprint(f, "slot:", key, ",value:")
						tmp := common.TrimLeftZeroes(value[:])
						if len(tmp) != 0 {
							fmt.Fprintf(f, "0x%v\n", common.Bytes2Hex(tmp))
							// s += fmt.Sprintf("0x%v\n", common.Bytes2Hex(tmp))
						} else {
							fmt.Fprintln(f, "0x0")
							// s += fmt.Sprintln("0x0")
						}
					}
				}

			}
		}

		if txlist, exist := common.BlockTxList[i]; exist {
			txcounter += len(common.BlockTxList[i])
			for _, tx := range txlist {
				// s := fmt.Sprintf("!TxHash:%v\n", tx)
				fmt.Fprintf(f, "!TxHash:%v\n", tx)
				txDetail := common.TxDetail[tx]

				// jhkim: transaction type
				if txDetail.Types == 1 {
					fmt.Fprintf(f, "Type:Transfer\n")
					// s += fmt.Sprintln("Type:Transfer")
					// s += fmt.Sprintln("  common.TxInformation: Transfer or Contract call")
				} else if txDetail.Types == 2 {
					fmt.Fprintf(f, "Type:ContractDeploy\n")
					// s += fmt.Sprintln("Type:ContractDeploy")
				} else if txDetail.Types == 3 {
					fmt.Fprintf(f, "Type:ContractCall\n")
					// s += fmt.Sprintln("Type:ContractCall")
				} else if txDetail.Types == 4 { // never enter this branch
					fmt.Fprintf(f, "Type:Failed\n")
					// s += fmt.Sprintln("Type:Failed")
					fmt.Println("txDetail.Types is 4, not changed from default", tx)
					os.Exit(0)
				} else if txDetail.Types == 41 {
					fmt.Fprintf(f, "Type:Failed_transfer\n")
					// s += fmt.Sprintln("Type:Failed_transfer")
				} else if txDetail.Types == 42 {
					fmt.Fprintf(f, "Type:Failed_contractdeploy\n")
					// s += fmt.Sprintln("Type:Failed_contractdeploy")
				} else if txDetail.Types == 43 {
					fmt.Fprintf(f, "Type:Failed_contractcall\n")
					// s += fmt.Sprintln("Type:Failed_contractcall")
				} else {
					fmt.Println("wrong Tx type", tx)
					os.Exit(0)
					fmt.Fprintf(f, "Wrong Tx Information")
					// s += fmt.Sprintln("Wrong Tx Information")
				}
				fmt.Fprintf(f, "From:%v\n", txDetail.From)
				// s += fmt.Sprintf("From:%v\n", txDetail.From)

				if txDetail.Types == 1 || txDetail.Types == 3 {
					fmt.Fprintf(f, "To:%v\n", txDetail.To)
					// s += fmt.Sprintf("To:%v\n", txDetail.To)
				} else if txDetail.Types == 2 {
					fmt.Fprintf(f, "DeployedCA:%v\n", txDetail.DeployedContractAddress)
					// s += fmt.Sprintf("DeployedCA:%v\n", txDetail.DeployedContractAddress)
				} else {
					// Do nothing
				}
				// s += fmt.Sprintln()
				// fmt.Fprint(f, s)

				readlist := common.TxReadList[tx]
				// fmt.Println("blocknumber", i, "readlist", readlist)
				fmt.Fprintf(f, "@ReadList\n")
				for addr := range readlist {
					fmt.Fprintf(f, ".address:%v\n", addr)
					// s += fmt.Sprintf(".address:%v\n", addr)
					// s += fmt.Sprintln("      Nonce:", stateAccount.Nonce)
					// s += fmt.Sprintln("      Balance:", stateAccount.Balance)
					// s += fmt.Sprintln("      CodeHash:", common.BytesToHash(stateAccount.CodeHash))

					// if stateAccount.Code != nil {
					// 	s += fmt.Sprintln("      Code:", common.Bytes2Hex(stateAccount.Code))
					// }
					// s += fmt.Sprintln("      StorageRoot:", stateAccount.StorageRoot)

					// s += fmt.Sprintln()
				}
				// s += fmt.Sprintln()
				// fmt.Fprint(f, s)
				writelist := common.TxWriteList[tx]
				// s = fmt.Sprintln("  WriteList")
				fmt.Fprintln(f, "#WriteList")
				for _, v := range txDetail.DeletedAddress {
					fmt.Fprintf(f, ".deletedaddress:%v\n", v)
				}

				for addr, stateAccount := range writelist {
					// if addr == common.HexToAddress("0x9a049f5d18C239EfAA258aF9f3E7002949a977a0") {
					// 	fmt.Println("writing..", tx, addr)
					// 	fmt.Println(stateAccount.Balance, stateAccount.CodeHash, stateAccount.Nonce, stateAccount.StorageRoot, stateAccount.Storage)
					// 	if stateAccount.Balance.Cmp(common.Big0) == 0 {
					// 		fmt.Println("Big.int good")
					// 	} else {
					// 		fmt.Println("Big.int bad")
					// 	}
					// }
					if stateAccount.CodeHash == common.EmptyCodeHash && len(stateAccount.Code) != 0 {
						fmt.Println("Something wrong with code and codehash - emptycodehash but no 0 length code")
						fmt.Println(i, tx, addr)
						os.Exit(0)
					}
					if stateAccount.CodeHash != common.EmptyCodeHash && len(stateAccount.Code) == 0 {
						fmt.Println("Something wrong with code and codehash - 0 length code but emptycodehash")
						fmt.Println(i, tx, addr)
						os.Exit(0)
					}
					if stateAccount.CodeHash == common.EmptyCodeHash {
						if stateAccount.Balance.Cmp(common.Big0) == 0 &&
							len(stateAccount.Code) == 0 &&
							stateAccount.Nonce == 0 &&
							stateAccount.StorageRoot == common.EmptyRoot {
							// empty account
							// do nothing
							// fmt.Println("Don't write empty account", i, tx, addr)

							// continue
							// 인줄 알았는데 46383 block에서 0x7a19C3C33f2a800b2FA361fD24277cAb0A1C1dB1 가 empty acc임에도 불구하고 write가 필요함
							// empty account에게 0 value를 보냈기 때문
							// deploy 실패로 만들어지는 empty account와는 다름

						}
					}
					if contains(txDetail.DeletedAddress, addr) {
						continue

					}
					if stateAccount != nil {
						// s = ""
						// if txDetail.Types == 42 && txDetail.From != addr {
						// 	// for block 46402, addr: 0x9a049f5d18C239EfAA258aF9f3E7002949a977a0
						// 	//	skip empty account of deploy fail tx.

						// 	// why block 312668, addr: 0xdeAff3ccA517bC15B74397D9a9248134484F70D9
						// 	// why doesn't filter?
						// 	if stateAccount.Balance.Cmp(common.Big0) == 0 &&
						// 		len(stateAccount.Code) == 0 &&
						// 		stateAccount.Nonce == 0 &&
						// 		stateAccount.StorageRoot == common.EmptyRoot {
						// 		// empty account
						// 		// do nothing
						// 		// fmt.Println("Don't write empty account", i, tx, addr)
						// 		fmt.Println("tx type: contract deploy fail", tx, addr)
						// 		continue
						// 	}
						// } else if txDetail.Types == 43 && txDetail.From != addr {
						// 	// for block 82776, addr: 0x8AFbB3091B7C0bb08D0724513c12f46816cF6e64
						// 	//	skip empty account of contract call fail

						// 	if stateAccount.Balance.Cmp(common.Big0) == 0 &&
						// 		len(stateAccount.Code) == 0 &&
						// 		stateAccount.Nonce == 0 &&
						// 		stateAccount.StorageRoot == common.EmptyRoot {
						// 		// empty account
						// 		// do nothing
						// 		// fmt.Println("Don't write empty account", i, tx, addr)
						// 		fmt.Println("tx type: contract call fail", tx, addr)
						// 		continue
						// 	}

						// } else if txDetail.To != addr {
						// 	if stateAccount.Balance.Cmp(common.Big0) == 0 &&
						// 		len(stateAccount.Code) == 0 &&
						// 		stateAccount.Nonce == 0 &&
						// 		stateAccount.StorageRoot == common.EmptyRoot {
						// 		// empty account
						// 		// do nothing

						// 		continue
						// 	}
						// }

						// fmt.Println("1", addr, txDetail.To)
						if stateAccount.Balance.Cmp(common.Big0) == 0 &&
							len(stateAccount.Code) == 0 &&
							stateAccount.Nonce == 0 &&
							stateAccount.StorageRoot == common.EmptyRoot {
							// empty account

							// if txDetail.To != addr || txDetail.DeployedContractAddress != addr || contains(txDetail.InternalDeployedAddress, addr) {
							// 	fmt.Println("2", addr, txDetail.To)

							// 	continue
							// }
							// if txDetail.Types == 1 { // transfer tx. send 0 value to empty account. doesn't skip

							// 	fmt.Println("2", addr, txDetail.To)

							// } else if txDetail.Types == 2 && txDetail.DeployedContractAddress == addr { // contract deploy tx. deploy empty contract but doesn't skip
							// 	fmt.Println("3", addr, txDetail.To)
							// 	// 	continue
							// }
						}
						if !contains(txDetail.InternalDeployedAddress, addr) {
							fmt.Fprintf(f, ".address:%v\n", addr)
							// s += fmt.Sprintf(".address:%v\n", addr)
						} else {
							fmt.Fprintf(f, ".Deployedaddress:%v\n", addr)
							// s += fmt.Sprintf("Deployedaddress:%v\n", addr)
						}

						fmt.Fprintf(f, "Nonce:%v\n", stateAccount.Nonce)
						// s += fmt.Sprintf("Nonce:%v\n", stateAccount.Nonce)
						fmt.Fprintf(f, "Balance:%v\n", stateAccount.Balance)
						// s += fmt.Sprintf("Balance:%v\n", stateAccount.Balance)
						if emptyCodeHash != stateAccount.CodeHash {
							fmt.Fprintf(f, "CodeHash:%v\n", stateAccount.CodeHash)
							// s += fmt.Sprintf("CodeHash:%v\n", common.BytesToHash(stateAccount.CodeHash))
						} else {
							fmt.Fprintf(f, "CodeHash:empty\n")
							// s += fmt.Sprintf("CodeHash:empty\n")
						}
						if txDetail.Types == 2 && txDetail.DeployedContractAddress == addr && stateAccount.Code != nil { // write hex contract code only deploy transaction
							// if stateAccount.Code != nil {
							// fmt.Printf("      Code:%v\n", common.Bytes2Hex(stateAccount.Code))
							if len(stateAccount.Code) != 0 {
								fmt.Fprintf(f, "Code:%v\n", common.Bytes2Hex(stateAccount.Code))
							} else {
								fmt.Fprintf(f, "Code:0x\n")
							}

							// s += fmt.Sprintf("Code:%v\n", common.Bytes2Hex(stateAccount.Code))
						} else if contains(txDetail.InternalDeployedAddress, addr) {
							if len(stateAccount.Code) != 0 {
								fmt.Fprintf(f, "Code:%v\n", common.Bytes2Hex(stateAccount.Code))
							} else {
								fmt.Fprintf(f, "Code:0x\n")
							}
							// s += fmt.Sprintf("Code:%v\n", common.Bytes2Hex(stateAccount.Code))
						}
						if types.EmptyRootHash != stateAccount.StorageRoot {
							fmt.Fprintf(f, "StorageRoot:%v\n", stateAccount.StorageRoot)
							// s += fmt.Sprintf("StorageRoot:%v\n", stateAccount.StorageRoot)
						} else {
							fmt.Fprintf(f, "StorageRoot:empty\n")
							// s += fmt.Sprintf("StorageRoot:empty\n")
						}

						common.PrettyTxWritePrint(tx, addr)

						if len(stateAccount.Storage) != 0 {
							fmt.Fprintf(f, "Storage:\n")
							// s += fmt.Sprintln("Storage:")
							for k, v := range stateAccount.Storage {
								// slice version

								fmt.Fprint(f, "slot:", k, ",value:")
								// s += fmt.Sprint("slot:", k, ",value:")
								tmp := common.TrimLeftZeroes(v[:])
								if len(tmp) != 0 {
									fmt.Fprintf(f, "0x%v\n", common.Bytes2Hex(tmp))
									// s += fmt.Sprintf("0x%v\n", common.Bytes2Hex(tmp))
								} else {
									fmt.Fprintln(f, "0x0")
									// s += fmt.Sprintln("0x0")
								}

								// // map version
								// for kk, vv := range v {
								// 	s += fmt.Sprint("          slot:", kk, ",value:")
								// 	tmp := common.TrimLeftZeroes(vv[:])
								// 	if len(tmp) != 0 {
								// 		s += fmt.Sprintf("0x%v\n", common.Bytes2Hex(tmp))
								// 	} else {
								// 		s += fmt.Sprintln("0x0")
								// 	}
								// }

								// s += fmt.Sprintln()
							}
						}

						// s += fmt.Sprintf("      RlpEncoded:0x%v\n", common.Bytes2Hex(RLPEncodeSubstateAccount(*stateAccount)))

						// fmt.Fprint(f, s)
						delete(common.TxWriteList[tx], addr)

					} else {
						fmt.Fprintf(f, ".deletedaddress:%v\n", addr)
					}
				}
				delete(common.TxWriteList, tx)
				// s += fmt.Sprintln()
				// fmt.Fprintln(f, s)

			}

		}

		// s += fmt.Sprintln("##########################################################################")
		if minerSA, exist := common.BlockMinerList[i]; exist {
			fmt.Fprintf(f, "$Miner:%v\n", minerSA.Addr)
			// s = fmt.Sprintf("$Miner:%v\n", minerSA.Addr)
			fmt.Fprintf(f, "Nonce:%v\n", minerSA.Nonce)
			// s += fmt.Sprintf("Nonce:%v\n", minerSA.Nonce)
			fmt.Fprintf(f, "Balance:%v\n", minerSA.Balance)
			// s += fmt.Sprintf("Balance:%v\n", minerSA.Balance)
			if emptyCodeHash != minerSA.Codehash {
				fmt.Fprintf(f, "Codehash:%v\n", minerSA.Codehash)
				// s += fmt.Sprintf("Codehash:%v\n", minerSA.Codehash)
			} else {
				fmt.Fprintf(f, "CodeHash:empty\n")
				// s += fmt.Sprintf("CodeHash:empty\n")
			}

			if types.EmptyRootHash != minerSA.StorageRoot {
				fmt.Fprintf(f, "StorageRoot:%v\n", minerSA.StorageRoot)
				// s += fmt.Sprintf("StorageRoot:%v\n", minerSA.StorageRoot)
			} else {
				fmt.Fprintf(f, "StorageRoot:empty\n")
				// s += fmt.Sprintf("StorageRoot:empty\n")
			}
		}
		// s += fmt.Sprintf("  RlpEncoded:0x%v\n", common.Bytes2Hex(RLPEncodeSimpleAccount(minerSA)))
		// fmt.Fprint(f, s)
		// s = fmt.Sprintln("Miner:", common.BlockMinerList[i].Addr, ",Balance:", common.BlockMinerList[i].Balance)
		if uncles, exist := common.BlockUncleList[i]; exist {
			if len(uncles) != 0 {
				for _, uncle := range uncles {
					fmt.Fprintf(f, "^Uncle:%v\n", uncle.Addr)
					// s = fmt.Sprintf("^Uncle:%v\n", uncle.Addr)
					fmt.Fprintf(f, "Nonce:%v\n", uncle.Nonce)
					// s += fmt.Sprintf("Nonce:%v\n", uncle.Nonce)
					fmt.Fprintf(f, "Balance:%v\n", uncle.Balance)
					// s += fmt.Sprintf("Balance:%v\n", uncle.Balance)
					if emptyCodeHash != uncle.Codehash {
						fmt.Fprintf(f, "Codehash:%v\n", uncle.Codehash)
						// s += fmt.Sprintf("Codehash:%v\n", uncle.Codehash)
					} else {
						fmt.Fprintf(f, "CodeHash:empty\n")
						// s += fmt.Sprintf("CodeHash:empty\n")
					}

					if types.EmptyRootHash != uncle.StorageRoot {
						fmt.Fprintf(f, "StorageRoot:%v\n", uncle.StorageRoot)
						// s += fmt.Sprintf("StorageRoot:%v\n", uncle.StorageRoot)
					} else {
						fmt.Fprintf(f, "StorageRoot:empty\n")
						// s += fmt.Sprintf("StorageRoot:empty\n")
					}

					// s += fmt.Sprintf("  RlpEncoded:0x%v\n", common.Bytes2Hex(RLPEncodeSimpleAccount(uncle)))
					// fmt.Fprint(f, s)
				}

			}
		}

		// s += fmt.Sprintln("##########################################################################")
		// fmt.Fprintln(f, s)
	}

	elapsed := time.Since(start)
	ss += fmt.Sprintln(" in", elapsed.Seconds(), "seconds")
	ss += fmt.Sprintln("file:", filepath)
	fmt.Print(ss)
	fmt.Println("# of Txs written in block", strconv.FormatInt(int64(blocknumber-distance+1), 10)+"-"+strconv.FormatInt(int64(blocknumber), 10), ":", txcounter)

}

func contains(list []common.Address, addr common.Address) bool {
	for _, v := range list {
		if v == addr {
			return true
		}
	}
	return false
}
