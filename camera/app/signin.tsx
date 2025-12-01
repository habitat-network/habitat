import { ThemedView } from "@/components/themed-view";
import { useState } from "react";
import { Button, TextInput } from "react-native";
import { useAuth } from "@/context/auth";
import { useRouter } from "expo-router";

const SignIn = () => {
  const [handle, setHandle] = useState("sashankg.bsky.social");
  const { signIn } = useAuth();
  const { replace } = useRouter();
  return (
    <ThemedView
      style={{ flex: 1, justifyContent: "center", alignItems: "center" }}
    >
      <TextInput
        onChangeText={setHandle}
        value={handle}
        style={{ backgroundColor: "gray" }}
      />
      <Button
        onPress={async () => {
          await signIn(handle);
          replace("/");
        }}
        title="Sign in"
      />
    </ThemedView>
  );
};

export default SignIn;
