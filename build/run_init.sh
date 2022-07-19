#!/bin/bash
export path=./bin/data
#read -p "Geth data dir: " path
make -C ../ geth
rm -rf $path/geth
./bin/geth --datadir $path init frontier.json
