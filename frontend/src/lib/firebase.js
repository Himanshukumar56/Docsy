// Import the functions you need from the SDKs you need
import { initializeApp } from "firebase/app";
import { getAuth } from "firebase/auth";

// Your web app's Firebase configuration
const firebaseConfig = {
  apiKey: "AIzaSyCY0NHhLGf6bINhMiqTrXJSvW_cLsLA3qs",
  authDomain: "docsy-5fea8.firebaseapp.com",
  projectId: "docsy-5fea8",
  storageBucket: "docsy-5fea8.firebasestorage.app",
  messagingSenderId: "367965249433",
  appId: "1:367965249433:web:b3dbee8e2f4540530f7d08",
  measurementId: "G-36353TPKD4",
};

// Initialize Firebase
const app = initializeApp(firebaseConfig);
const auth = getAuth(app);

export { auth };
export default app;
