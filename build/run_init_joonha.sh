#!/bin/bash
cd ../ && make geth
cd build
export path=/ethereum/geth-test-joonha
#read -p "Geth data dir: " path
rm -rf $path/geth
./bin/geth --datadir $path init genesis.json