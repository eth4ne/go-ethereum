#!/bin/sh
cd ../..
make geth
cd build/bin

DataDir="/ethereum/geth-full-v1.10.16"


if [ -z "$2" ]
then
  echo "Start replaying Ethereum block #" $1
  echo ./geth --datadir ${DataDir} replayblock $1
  sleep 5  
  ./geth --datadir ${DataDir} replayblock $1
else
  echo "Start replaying Ethereum blocks #" $1 "~" $2
  echo ./geth --datadir ${DataDir} replayblock $1 $2
  sleep 5  
  ./geth --datadir ${DataDir} replayblock $1 $2
fi




#echo "clear chaindata"


#./geth  --datadir ${DataDir} --syncmode=full --gcmode=archive \
#./geth --datadir ${DataDir} --syncmode=full --http --http.port "8082" --http.corsdomain="*" --http.api="admin,eth,debug,miner,net,txpool,personal,web3" \
 #      --cache 100000 --cache.database 80 --cache.trie 50 \
  #     --snapshot=false


