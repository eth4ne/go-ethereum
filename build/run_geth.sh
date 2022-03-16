#!/bin/bash
read -p "Geth data dir: " path
./bin/geth --syncmode full --gcmode archive --datadir $path --ws --ws.port 8548 --ws.api admin,debug,web3,eth,txpool,personal,ethash,miner,net --cache 60000 --networkid 1024 --port 30310 --allow-insecure-unlock --maxpeers 0 --verbosity 2 --discovery.dns "" --nodiscover
#>> test.txt 2>debug.txt
# --pprof --pprof.addr 0.0.0.0 --pprof.port 7778 --metrics --metrics.addr 0.0.0.0
