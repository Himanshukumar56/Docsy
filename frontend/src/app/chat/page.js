// app/chat/page.js
"use client";

import { Suspense } from "react";
import Chat from "./chat";

function ChatPageContent() {
  return <Chat />;
}

export default function ChatPage() {
  return (
    <Suspense fallback={<div>Loading...</div>}>
      <ChatPageContent />
    </Suspense>
  );
}
