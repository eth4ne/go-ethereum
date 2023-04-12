#!/bin/sh
cd ../..
make geth
cd build/bin

#DataDir="/ethereum/geth-full-v1.10.16"
# DataDir="/ethereum/geth" # already archived 10M
DataDir="/ethereum/geth-tmp/"
echo "\n***WARNING***"
echo "Start deleting archive Data in 5 seconds"
echo "***WARNING***\n"
sleep 1
echo "1"
sleep 1
echo "2"
sleep 1
echo "3"
sleep 1
echo "4"
sleep 1
echo "5"
echo "start deleting"
#rm -rf ${DataDir} && mkdir ${DataDir}
echo "clear chaindata"


#./geth  --datadir ${DataDir} --syncmode=full --gcmode=archive \
./geth --datadir ${DataDir} --syncmode=full  --http --http.port "8084" --http.corsdomain="*" --http.api="admin,eth,debug,miner,net,txpool,personal,web3" \
       --cache 30000 \
       --snapshot=false --port 30310

# ./geth  --datadir ${DataDir} --syncmode=full --http --http.port "8081" --http.corsdomain="*" --http.api="admin,eth,debug,miner,net,txpool,personal,web3" --snapshot=false --port 30304
