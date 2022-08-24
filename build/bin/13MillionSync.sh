echo "***WARNING***"
echo "This script is used for already synced geth"
sleep 5


cd ../..
make geth
cd build/bin


# DataDir="/ethereum/geth-txsubstate"
#DataDir="/ssd/ethereum-vanilla"
# ***WARNING*** #
DataDir="/ethereum/geth" # procyon server, already synced


./geth  --datadir ${DataDir} --syncmode=full --gcmode=archive --http --http.port "8081" --http.corsdomain="*" --http.api="admin,eth,debug,miner,net,txpool,personal,web3" --snapshot=false

