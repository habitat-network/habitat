import { ThemedText } from "@/components/themed-text";
import { ThemedView } from "@/components/themed-view";
import { domain, useAuth } from "@/context/auth";
import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import {
  Image,
  ImageSourcePropType,
  Platform,
  useWindowDimensions,
} from "react-native";

const Tile = ({ cid, size }: { cid: string; size: number }) => {
  const { token, fetchWithAuth, did } = useAuth();
  const src = `https://${domain}/xrpc/network.habitat.getBlob?cid=${cid}&did=${did}`;
  const headers = {
    Authorization: `Bearer ${token}`,
    "Habitat-Auth-Method": "oauth",
  };

  const [imgSrc, setImgSrc] = useState<ImageSourcePropType>();
  useEffect(() => {
    let cancelled = false;
    if (Platform.OS === "web") {
      fetchWithAuth(src, { headers })
        .then((res) => res.blob())
        .then((blob) => {
          const reader = new FileReader();
          reader.onloadend = () => {
            if (!cancelled && reader.result) {
              const uri = reader.result.toString();
              setImgSrc({ uri: uri });
            }
          };
          reader.readAsDataURL(blob);
        });
      return () => {
        cancelled = true;
      };
    } else {
      // Native can use the src directly
      setImgSrc({
        uri: src,
        headers: headers,
      }); // Native can use the URL directly
    }
  }, [src, fetchWithAuth]);

  if (!imgSrc) return null; // or a loading spinner

  return (
    <Image
      source={imgSrc}
      style={{
        width: size,
        height: size,
      }}
      resizeMode="cover"
    />
  );
};

const Photos = () => {
  const { fetchWithAuth, did } = useAuth();
  const {
    isLoading,
    data: photos,
    error,
  } = useQuery({
    queryKey: ["photos"],
    queryFn: async () => {
      const res = await fetchWithAuth(
        `/xrpc/network.habitat.listRecords?collection=network.habitat.photo&repo=${did}`, // TODO: repo
      );
      if (!res || !res.ok) {
        throw new Error(
          "fetching photos: " + res.statusText + (await res.text()),
        );
      }
      const list = await res.json();
      return list["records"] as { value: { ref: string } }[];
    },
  });
  const { width } = useWindowDimensions();
  // Determine tiles per row
  const tilesPerRow = Platform.OS === "web" ? 10 : 3;
  // Calculate tile width
  const tileSize = width / tilesPerRow;

  if (error) {
    return <ThemedText>{error.message}</ThemedText>;
  }

  if (!photos || isLoading) {
    return <ThemedText>Loading ... </ThemedText>;
  }

  return (
    <ThemedView style={{ flexDirection: "row", flexWrap: "wrap" }}>
      {photos.map(({ value }, i) => (
        <Tile key={i} cid={value.ref} size={tileSize} />
      ))}
    </ThemedView>
  );
};

export default Photos;
