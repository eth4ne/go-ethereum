#!/bin/bash
read -p "Geth data dir: " path
rm -rf $path/geth
./bin/geth --datadir $path init genesis.json
