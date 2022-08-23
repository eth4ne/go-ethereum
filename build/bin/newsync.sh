cd ../..
make geth
cd build/bin


#  DataDir="/ethereum/geth-txsubstate" #procyon
DataDir="/ssd/ethereum-vanilla" #lynx
echo "clear chaindata"
rm -rf ${DataDir} && mkdir ${DataDir}
#./geth --verbosity=4 --datadir ${DataDir} --syncmode=full --gcmode=archive --http --http.port "8081" --http.corsdomain="*" --http.api="admin,eth,debug,miner,net,txpool,personal,web3" --snapshot=false
./geth  --datadir ${DataDir} --syncmode=full --gcmode=archive --http --http.port "8081" --http.corsdomain="*" --http.api="admin,eth,debug,miner,net,txpool,personal,web3" --snapshot=false

