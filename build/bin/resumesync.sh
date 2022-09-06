#!/bin/sh
cd ../..
make geth
cd build/bin


#  DataDir="/ethereum/geth-txsubstate" #procyon
#DataDir="/ssd/ethereum-vanilla" #lynx
DataDir="/ethereum/geth"

./geth  --datadir ${DataDir} --syncmode=full --gcmode=archive --http --http.port "8081" --http.corsdomain="*" --http.api="admin,eth,debug,miner,net,txpool,personal,web3" --snapshot=false --port 30304

