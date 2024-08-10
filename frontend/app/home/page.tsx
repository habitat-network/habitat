'use client'

import React from 'react';
import  withAuth from '@/components/withAuth';
import { useAuth } from '@/components/authContext';

const Home: React.FC = () => {
  const { logout } = useAuth();

  return (
    <main
      className="flex items-center justify-center w-full h-screen flex-col gap-4"
    >
      <div className="text-5xl flex flex-col items-center">
        <span>🌱</span>
        <h1>Filler Home Page</h1>
      </div>
      <button onClick={logout} >
            Logout
        </button>
    </main>
  );
};

export default withAuth(Home);
