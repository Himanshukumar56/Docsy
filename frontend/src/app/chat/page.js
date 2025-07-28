// app/chat/page.js
import { Suspense } from "react";
import Chat from "./chat";

export default function ChatPage() {
  return (
    <Suspense fallback={<div>Loading...</div>}>
      <Chat />
    </Suspense>
  );
}
