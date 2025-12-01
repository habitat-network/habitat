import {
  DarkTheme,
  DefaultTheme,
  ThemeProvider,
} from "@react-navigation/native";
import { SplashScreen, Stack } from "expo-router";
import { StatusBar } from "expo-status-bar";
import "react-native-reanimated";
import { useSafeAreaInsets } from "react-native-safe-area-context";

import { useColorScheme } from "@/hooks/use-color-scheme";
import { ThemedView } from "@/components/themed-view";
import { AuthProvider, useAuth } from "@/context/auth";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

export const unstable_settings = {
  anchor: "(tabs)",
};

const queryClient = new QueryClient();

SplashScreen.preventAutoHideAsync();

export default function RootLayout() {
  const colorScheme = useColorScheme();
  const { bottom } = useSafeAreaInsets();
  return (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <SplashScreenController />
        <ThemeProvider
          value={colorScheme === "dark" ? DarkTheme : DefaultTheme}
        >
          <ThemedView style={{ flex: 1, paddingBottom: bottom }}>
            <RootNavigator />
          </ThemedView>
          <StatusBar style="auto" />
        </ThemeProvider>
      </AuthProvider>
    </QueryClientProvider>
  );
}

export function SplashScreenController() {
  const { isLoading } = useAuth();
  if (!isLoading) {
    SplashScreen.hide();
  }
  return null;
}

const RootNavigator = () => {
  const { token, isLoading } = useAuth();
  return (
    <Stack
      screenOptions={{
        headerShown: false,
      }}
    >
      <Stack.Protected guard={!!token}>
        <Stack.Screen name="(app)" />
      </Stack.Protected>
      <Stack.Protected guard={!token}>
        <Stack.Screen name="signin" />
      </Stack.Protected>
    </Stack>
  );
};
