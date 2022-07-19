#!/bin/bash
export path=./bin/data
#read -p "Geth data dir: " path
./bin/geth --syncmode full --gcmode archive --datadir $path --cache 60000 --port 30304 --allow-insecure-unlock --maxpeers 0 --verbosity 5 --discovery.dns "" --nodiscover
#>> test.txt 2>debug.txt
# --pprof --pprof.addr 0.0.0.0 --pprof.port 7778 --metrics --metrics.addr 0.0.0.0
