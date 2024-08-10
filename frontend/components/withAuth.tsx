'use client'

import React, { useEffect } from 'react';
import { useRouter } from 'next/navigation';
import { useAuth } from './authContext';

const withAuth = (WrappedComponent: React.FC) => {
    const ComponentWithAuth = (props: any) => {
        const { isAuthenticated } = useAuth();
        const router = useRouter();

        useEffect(() => {
            console.log(isAuthenticated);
            if (!isAuthenticated) {
                console.log("pusho");
                router.push('/login');
            }
        }, [isAuthenticated, router]);

        return <WrappedComponent {...props} />;
    };

    return ComponentWithAuth;
};

export default withAuth;