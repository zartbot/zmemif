
# zMemif
zMemif is a native golang based library for [memif](https://doc.dpdk.org/guides/nics/memif.html) to interworking with dpdk.
it can simply provide 20Mpps recv and 10Mpps xmit capability.


* The golang lib is forked from `vpp gomemif` example code, and modified the "Interface" to "Port" which make golang developers more clear about the definition. it also change the "master/slave" to "server/client" to align with existing dpdk library.
* we write a dpdk based light-weight forwarder to replace vpp on data path, provide simple virtio-user interface for kernel service and tcp based remote mgmt usage, all udp packet will redirect to memif which can be directly serve by native golang.


## Motivation

Many applications like network telemetry analysis, video processing and other udp based application may require high performance I/O support in native-golang enviroment. DPDK may useful to accelerate packet proccessing in these case, intel open souce nff-go project few years ago, however the cgo and flow based abstraction may useful for NFV developer, but many application developer require a simple native go based solution.

This project wil be used for [ruta-io](https://github.com/ruta-io) and [netDAM](http://arxiv.org/abs/2110.14902).
We may integrate `zMemif` with `go-quic` in the future to provide `quic based segment routing(quic-sr)` or `Segment Routing over UDP(SRoU)`.


## Introduction

zmemif is a memory interface implementation for accelerate golang.
You could build your application pure in golang mode, and use DPDK as memif server.

## Usage

### 1. native golang server <----memif----> native golang client

example could be found /example/simple_echo  and /example/bw_test

### 2. DPDK based server <----memif----> native golang client

dpdk forwarder server could be found /dpdk folder.

`main.c` defined the RX/TX core and queue per core, will add by cli args in the future

```c
    unsigned int first_lcore = 25;
    char *ipaddr = "11.11.11.11";
    char *netmask = "255.255.255.0";
    int num_memif = 16;
    int num_rx_core = 4;
    int num_tx_core = 2;
    int server_mode = 1;
```

how to compile dpdk forwarder

1. install and setup dpdk env could be found in the following url

https://github.com/zartbot/learn_dpdk/tree/main/a1_setup_mlx5_sriov_env

2. compile and run

```bash
cd dpdk
make
sudo ./build/run
```

3. about unix sock file
by default, it will create `/tmp/memif.sock` file for unix socket to communicate with golang client, this unix socket is used to allocate memory region and ring buffer. DPDK forwarder need to run in root mode and auto create this file, so this file must provide access priviledge for golang client.

```bash
sudo chmod 777 /tmp/memif.sock 
```
4. run native golang client

```bash
cd /example/dpdk_co_worker
```

run send/recv examples..


## Roadmap
1. reliable transmit on datapath
2. simple udp level send/recv warpper for application migration.
3. In the future , we will use the [netDAM](http://arxiv.org/abs/2110.14902) DPU hardware to replace the dpdk based forwarder and provide full native userspace memif access.

## Reference 

* [memif](https://doc.dpdk.org/guides/nics/memif.html)
* [virtio-user dpdk usage](https://doc.dpdk.org/guides/howto/virtio_user_as_exceptional_path.html#sample-usage)

* [netDAM](http://arxiv.org/abs/2110.14902)
* [ruta-io](https://github.com/ruta-io)