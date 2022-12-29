#!/bin/sh
cd ../..
make geth
cd build/bin

DataDir="/ethereum/geth-full-v1.10.16"

echo "\n***WARNING***"
echo "Start deleting archive Data in 5 seconds"
echo "***WARNING***\n"
sleep 5

rm -rf ${DataDir} && mkdir ${DataDir}
echo "clear chaindata"


#./geth  --datadir ${DataDir} --syncmode=full --gcmode=archive \
./geth --datadir ${DataDir} --syncmode=full --http --http.port "8082" --http.corsdomain="*" --http.api="admin,eth,debug,miner,net,txpool,personal,web3" \
       --cache 100000 --cache.database 80 --cache.trie 50 \
       --snapshot=false

# ./geth  --datadir ${DataDir} --syncmode=full --http --http.port "8081" --http.corsdomain="*" --http.api="admin,eth,debug,miner,net,txpool,personal,web3" --snapshot=false --port 30304
