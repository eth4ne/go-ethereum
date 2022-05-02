## Geth for benchmark

* forked from 1.10.16-stable

### Setup

* Build Geth.
```
make geth
```
* Create an arbitrary coinbase wallet from RPC, and fill the address into ```build/genesis.json```.
* Set the coinbase wallet password in ```simulation.py```.
* Set the Geth data directory in ```run_init.sh```, and ```run_geth.sh```.
* Write the genesis by executing ```run_init.sh```.
* Start the Geth with ```run_geth.sh```.