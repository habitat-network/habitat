This is how it would work "in the wild".
 The ideal is everyone runs their own privi instance, wrapping private data.

 [Arushi Private PDS][Arushi Privi]
 [Sashank Private PDS][Sashanki Privi]

 If Arushi wants to request private data from Sashank's PPDS:
 0. Arushi is given Sashank's DID.
 1. Arushi resolves Sashank's DID document to get Sashank Privi DID.
 2. Arushi requests her own PDS with atproto-proxy header containing Sashank Privi DID.
 3. Arushi PDS signs the request (jwt) which means request is coming from me.
 4. Arushi PDS forwards the request to Sashank's Privi instance via DID resolve.
 5. Sashank's Privi checks permissions and returns result to Arushi

The Privi instances actually don't need to be co-located with the PDS, but wherever the DID docs point to, that Privi needs to be hosting that DID user's Privi.