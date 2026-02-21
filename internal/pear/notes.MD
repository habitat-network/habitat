This is how it would work "in the wild".
The ideal is everyone runs their own pear instance, wrapping private data.

[Arushi Private PDS][Arushi Pear]
[Sashank Private PDS][Sashanki Pear]

If Arushi wants to request private data from Sashank's PPDS: 0. Arushi is given Sashank's DID.

1.  Arushi resolves Sashank's DID document to get Sashank Pear DID.
2.  Arushi requests her own PDS with atproto-proxy header containing Sashank Pear DID.
3.  Arushi PDS signs the request (jwt) which means request is coming from me.
4.  Arushi PDS forwards the request to Sashank's Pear instance via DID resolve.
5.  Sashank's Pear checks permissions and returns result to Arushi

The Pear instances actually don't need to be co-located with the PDS, but wherever the DID docs point to, that Pear needs to be hosting that DID user's Pear.
