package main

import (
	"fmt"
	"os"
	"strconv"
)

var Path = "/shared/jhkim/"

func main() {
	args := os.Args[1:]

	end, err := strconv.Atoi(args[0]) // end block number
	if err != nil {
		fmt.Println("Wrong end block number", args[0])
	}
	epoch := 1000
	for i := 1; i < end; i += epoch {
		section := fmt.Sprintf("%08d", i) + "-" + fmt.Sprintf("%08d", i+epoch-1)
		fmt.Println("CHECK FILE", Path+"TxSubstate"+section+".txt")
		newfile := Path + "TxSubstate" + section + ".txt"
		file := Path + "backup_00000001_01919000/TxSubstate" + section + ".txt"

		stat_new, err := os.Stat(newfile)
		if err != nil {
			fmt.Println("cannot open file", newfile)
			os.Exit(1)
		}
		stat, err := os.Stat(file)
		if err != nil {
			fmt.Println("cannot open file", file)
			os.Exit(1)
		}
		if stat_new.Size() != stat.Size() {
			fmt.Println("NEW FILE SIZE:", stat_new.Size())
			fmt.Println("OLD FILE SIZE:", stat.Size())
			os.Exit(0)
		}

		// f_new, err := os.Open(newfile)
		// if err != nil {
		// 	fmt.Println("cannot open file", Path+"TxSubstate"+section+".txt")
		// 	os.Exit(1)
		// }
		// f, err := os.Open(file)
		// if err != nil {
		// 	fmt.Println("cannot open file", Path+"backup_00000001_01919000/TxSubstate"+section+".txt")
		// 	os.Exit(1)
		// }

		// scanner_new := bufio.NewScanner(f_new)
		// scanner := bufio.NewScanner(f)

		// for scanner_new.Scan() {
		// 	scanner.Scan()
		// 	// fmt.Println(scanner_new.Text())
		// 	// fmt.Println(scanner.Text())
		// 	if scanner.Text() != scanner_new.Text() {
		// 		fmt.Println("WRONG TXSUBSTATE")
		// 		fmt.Println("REAL ANSWER:", scanner.Text())
		// 		fmt.Println("YOUR ANSWER:", scanner_new.Text())

		// 		os.Exit(0)

		// 	}
		// }
	}

}
