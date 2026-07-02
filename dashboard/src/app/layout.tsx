import type { Metadata } from 'next';
import { Fira_Sans, Fira_Code } from 'next/font/google';
import { AuthBootstrap } from '@/components/AuthBootstrap';
import './globals.css';

const firaSans = Fira_Sans({
  subsets: ['latin'],
  weight: ['300', '400', '500', '600', '700'],
  variable: '--font-sans',
  display: 'swap',
});

const firaCode = Fira_Code({
  subsets: ['latin'],
  weight: ['400', '500', '600'],
  variable: '--font-mono',
  display: 'swap',
});

export const metadata: Metadata = {
  title: 'Backend Monitoring Dashboard',
  description: 'Torrent Search Go backend monitoring and operations dashboard.',
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" className={`dark ${firaSans.variable} ${firaCode.variable}`}>
      <body>
        <AuthBootstrap />
        {children}
      </body>
    </html>
  );
}