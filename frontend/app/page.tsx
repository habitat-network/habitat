'use client'

import React from 'react';
import Link from 'next/link';
import  withAuth from '@/components/withAuth';
import { useAuth } from '@/components/authContext';
import { getWebApps } from '@/api/node';
import { useRouter } from 'next/navigation';
import './home.css';

const Home: React.FC = () => {
  const { logout } = useAuth();
  const [webApps, setWebApps] = React.useState<any[]>([]);

  const [isLoading, setIsLoading] = React.useState<boolean>(true);

  React.useEffect(() => {
    const fetchApps = async () => {
      setIsLoading(true);
      try {
        const webAppInstallations = await getWebApps();
        const filteredWebApps = webAppInstallations
          .filter((app: any) => app.driver === 'web')
          .map((app: any) => ({
            id: app.id,
            name: app.name,
            description: 'No description available',
            icon: '🌐', // Default icon for web apps
            link: app.url || '#'
          }));
        setWebApps(filteredWebApps);
        setIsLoading(false);
      } catch (error) {
        console.error('Error fetching node state:', error);
      }
    };

    fetchApps();
  }, []);

  const router = useRouter();

  const myServerApp = {
    id: 'my-server',
    name: 'My Server',
    description: 'Manage your server',
    icon: '🖥️',
    link: '/server'
  };

  const appShopApp = {
    id: 'app-shop',
    name: 'App Gallery',
    description: 'Find apps to install on your server',
    icon: '🍎',
    link: '/app-store'
  }

  const apps = [myServerApp, appShopApp, ...webApps];

  return (
    <main
      className="flex items-center justify-start w-full h-screen flex-col gap-4"
    >
      <div className="flex flex-wrap justify-center gap-6 w-full max-w-4xl">
        {isLoading ? <div>Loading...</div> : apps.map((app) => (
          <Link
            href={app.link}
            key={app.id}
            className="w-64 h-64 bg-white shadow-lg rounded-lg flex items-center justify-center cursor-pointer hover:shadow-xl transition-shadow duration-300"
            onClick={(e) => {
              e.preventDefault();
              router.push(app.link);
            }}
          >
            <div className="text-center">
              <div className="text-4xl mb-2">{app.icon}</div>
              <h2 className="text-xl font-semibold">{app.name}</h2>
              <p className="text-sm text-gray-600">{app.description}</p>
            </div>
          </Link>
        ))}
      </div>
    </main>
  );
};

export default withAuth(Home);
