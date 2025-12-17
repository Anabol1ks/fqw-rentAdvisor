import './globals.css';
import type { Metadata } from 'next';
import { Inter } from 'next/font/google';

const inter = Inter({ subsets: ['latin'] });

export const metadata: Metadata = {
  title: 'RentAdvisor - Нейросетевой прогноз стоимости аренды',
  description: 'Система оценки стоимости аренды квартир в Москве на основе машинного обучения. Дипломный проект РТУ МИРЭА.',
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="ru">
      <body className={inter.className}>{children}</body>
    </html>
  );
}
