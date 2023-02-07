#!/bin/bash
export path=/ethereum/joonha/data
#read -p "Geth data dir: " path
./bin/geth --syncmode full --gcmode archive --datadir $path --cache 60000 --port 30304 --allow-insecure-unlock --maxpeers 0 --verbosity 5 --discovery.dns "" --nodiscover --snapshot=false --activeSnapshot=false --inactiveStorageSnapshot=false --inactiveAccountSnapshot=false --miner.noverify console
