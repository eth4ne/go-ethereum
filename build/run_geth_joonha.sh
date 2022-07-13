#!/bin/bash
export path=/ethereum/geth-test-joonha
#read -p "Geth data dir: " path
# ./bin/geth --syncmode full --gcmode archive --datadir $path --ws --ws.port 8550 --ws.api admin,debug,web3,eth,txpool,personal,ethash,miner,net --cache 60000 --networkid 1024 --port 30311 --allow-insecure-unlock --maxpeers 0 --verbosity 3 --discovery.dns "" --nodiscover console
./bin/geth --syncmode full --gcmode archive --datadir $path --http --http.port 8550 --http.api admin,debug,web3,eth,txpool,personal,ethash,miner,net --cache 60000 --networkid 1024 --port 30311 --allow-insecure-unlock --maxpeers 0 --verbosity 5 --discovery.dns "" --nodiscover --snapshot=false --activeSnapshot=false --inactiveStorageSnapshot=false console
#>> test.txt 2>debug.txt
# --pprof --pprof.addr 0.0.0.0 --pprof.port 7778 --metrics --metrics.addr 0.0.0.0