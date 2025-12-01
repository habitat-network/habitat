import { ThemedText } from "@/components/themed-text";
import { ThemedView } from "@/components/themed-view";
import { FetchWithAuth, useAuth } from "@/context/auth";
import {
  CameraCapturedPicture,
  CameraType,
  CameraView,
  useCameraPermissions,
} from "expo-camera";
import { Stack, useNavigation, useRouter } from "expo-router";
import { useRef, useEffect, useState } from "react";
import { Button, Platform, TouchableHighlight } from "react-native";

const cleanBase64 = (data: string) => {
  if (data.startsWith("data:")) {
    return data.substring(data.indexOf(",") + 1);
  }
  return data;
};

const Home = () => {
  const cameraRef = useRef<CameraView>(null);
  const router = useRouter();
  const [permission, requestPermission] = useCameraPermissions();
  const [facing, setFacing] = useState<CameraType>("back");
  const { fetchWithAuth, did } = useAuth();

  // Can't use form data because we are uploading directly to uploadBlob, not a special endpoint for photos
  const uploadPhoto = async (photo: CameraCapturedPicture) => {
    // Convert base64 to binary
    try {
      const base64 = cleanBase64(photo.base64!);
      const binary = atob(base64);
      const len = binary.length;
      const bytes = new Uint8Array(len);
      for (let i = 0; i < len; i++) {
        bytes[i] = binary.charCodeAt(i);
      }
      // Upload without FormData
      const res = await fetchWithAuth("/xrpc/network.habitat.uploadBlob", {
        method: "POST",
        headers: {
          "Content-Type": `image/jpeg`,
        },
        body: bytes, // raw jpeg bytes
      });

      if (!res || !res.ok) {
        throw new Error(
          "uploading photo blob: " + res.statusText + (await res.text()),
        );
      }

      const upload = await res.json();
      const cid = upload["blob"]["cid"]["$link"];

      if (cid === "") {
        throw new Error("upload blob returned empty cid");
      }

      const res2 = await fetchWithAuth("/xrpc/network.habitat.putRecord", {
        method: "POST",
        body: JSON.stringify({
          collection: "network.habitat.photo",
          record: {
            ref: cid,
          },
          repo: did,
        }),
      });

      if (!res2 || !res2.ok) {
        throw new Error("uploading photo record");
      }
    } catch (e) {
      console.error("Unable to upload photo because: ", e);
    }
  };

  useEffect(() => {
    if (!permission?.granted) {
      requestPermission();
    }
  }, [permission]);

  if (!permission || !permission.granted) {
    // Camera permissions are still loading
    return <ThemedText>no permissions</ThemedText>;
  }

  return (
    <ThemedView style={{ flex: 1, alignItems: "center" }}>
      <Stack.Screen
        options={{
          title: "Camera",
          headerLeft: () => (
            <TouchableHighlight onPress={() => router.navigate("/photos")}>
              <ThemedText>My Photos</ThemedText>
            </TouchableHighlight>
          ),

          headerRight: () => (
            <TouchableHighlight onPress={() => router.navigate("/signin")}>
              <ThemedText>Sign in</ThemedText>
            </TouchableHighlight>
          ),
        }}
      />
      <CameraView
        style={{ flex: 1, width: "100%" }}
        facing={facing}
        ref={cameraRef}
        active={true}
      />

      <TouchableHighlight
        onPress={async () => {
          const photo = await cameraRef.current?.takePictureAsync({
            base64: true,
            imageType: "jpg",
          });
          if (!photo) {
            console.error("camera.takePictureAsync returned undefined");
          } else {
            uploadPhoto(photo);
          }
        }}
        style={{ padding: 8 }}
      >
        <ThemedText>Capture</ThemedText>
      </TouchableHighlight>

      <TouchableHighlight
        onPress={() => {
          setFacing((facing) => (facing === "back" ? "front" : "back"));
        }}
        style={{ padding: 8 }}
      >
        <ThemedText>Flip Camera</ThemedText>
      </TouchableHighlight>
    </ThemedView>
  );
};

export default Home;
