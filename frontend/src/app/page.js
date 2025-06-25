// app/page.js

"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { onAuthStateChanged } from "firebase/auth";
import { auth } from "../lib/firebase";
import { AuthComponent } from "../components/authentication";

export default function Home() {
  const router = useRouter();

  useEffect(() => {
    const unsubscribe = onAuthStateChanged(auth, (user) => {
      if (user) {
        // If logged in, redirect to dashboard
        router.push("/dashboard");
      }
    });

    return () => unsubscribe(); // Cleanup listener
  }, [router]);

  return <AuthComponent />;
}
