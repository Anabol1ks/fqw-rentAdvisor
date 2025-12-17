'use client'

import { useState, useEffect } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { AddressAutocomplete } from '@/components/address-autocomplete'
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from '@/components/ui/select'
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from '@/components/ui/card'
import {
	api,
	AddressValuationRequest,
	AddressValuationResponse,
	AddressSuggestion,
} from '@/lib/api'
import { Loader2 } from 'lucide-react'
import { toast } from 'sonner'

const CONDITION_OPTIONS = [
	'ремонт не нужен',
	'косметический ремонт',
	'косметический',
	'евро',
	'дизайнерский',
]

// Функции для работы с cookies
const getCookie = (name: string): string | null => {
	if (typeof document === 'undefined') return null
	const value = `; ${document.cookie}`
	const parts = value.split(`; ${name}=`)
	if (parts.length === 2) return parts.pop()?.split(';').shift() || null
	return null
}

const setCookie = (name: string, value: string, days: number = 365) => {
	if (typeof document === 'undefined') return
	const date = new Date()
	date.setTime(date.getTime() + days * 24 * 60 * 60 * 1000)
	const expires = `expires=${date.toUTCString()}`
	document.cookie = `${name}=${value};${expires};path=/`
}

interface ValuationFormProps {
	onSuccess?: (response: AddressValuationResponse) => void
	onLoadingStart?: () => void
}

export function ValuationForm({
	onSuccess,
	onLoadingStart,
}: ValuationFormProps) {
	const [loading, setLoading] = useState(false)
	const [withText, setWithText] = useState(true)
	const [formData, setFormData] = useState<Partial<AddressValuationRequest>>({
		city: 'Москва',
	})

	// Загружаем состояние with_text из cookies при монтировании
	useEffect(() => {
		const savedValue = getCookie('with_text')
		if (savedValue !== null) {
			setWithText(savedValue === 'true')
		}
	}, [])

	// Сохраняем состояние with_text в cookies при изменении
	const handleWithTextChange = (checked: boolean) => {
		setWithText(checked)
		setCookie('with_text', checked.toString())
	}

	const handleSubmit = async (e: React.FormEvent) => {
		e.preventDefault()

		if (
			!formData.address ||
			formData.rooms === undefined ||
			!formData.area_total
		) {
			toast.error('Заполните обязательные поля')
			return
		}

		setLoading(true)
		onLoadingStart?.()
		try {
			const response = await api.createValuation({
				...formData,
				city: formData.city || 'Москва',
				with_text: withText,
			} as AddressValuationRequest)

			toast.success('Оценка успешно создана')
			onSuccess?.(response)
		} catch (error) {
			toast.error(
				error instanceof Error ? error.message : 'Ошибка при создании оценки'
			)
		} finally {
			setLoading(false)
		}
	}

	const updateField = (field: keyof AddressValuationRequest, value: any) => {
		setFormData(prev => ({ ...prev, [field]: value }))
	}

	// Обработчик выбора адреса (только заполнение адреса)
	const handleAddressSelect = (suggestion: AddressSuggestion) => {
		// Yandex Geocoder API не предоставляет данные о доме
		// Для автозаполнения этих полей нужен другой API
		setFormData(prev => ({ ...prev, address: suggestion.address }))
	}

	return (
		<Card className='w-full'>
			<CardHeader>
				<CardTitle>Оценка стоимости аренды</CardTitle>
				<CardDescription>
					Заполните параметры квартиры для получения прогноза стоимости аренды
				</CardDescription>
			</CardHeader>
			<CardContent>
				<form onSubmit={handleSubmit} className='space-y-4'>
					<div className='grid grid-cols-1 md:grid-cols-2 gap-4'>
						<div className='space-y-2'>
							<Label htmlFor='address'>Адрес *</Label>
							<AddressAutocomplete
								id='address'
								placeholder='Нежинская улица, 1к1'
								value={formData.address || ''}
								onChange={value => updateField('address', value)}
								onSelect={handleAddressSelect}
								required
							/>
						</div>

						<div className='space-y-2'>
							<Label htmlFor='city'>Город *</Label>
							<Input
								id='city'
								placeholder='Москва'
								value={formData.city || 'Москва'}
								readOnly
								className='bg-muted/50 cursor-not-allowed'
								required
							/>
						</div>

						<div className='space-y-2'>
							<Label htmlFor='rooms'>Количество комнат * (0 - студия)</Label>
							<Input
								id='rooms'
								type='number'
								min='0'
								placeholder='2'
								value={formData.rooms === undefined ? '' : formData.rooms}
								onChange={e => {
									const val = e.target.value
									updateField('rooms', val === '' ? undefined : parseInt(val))
								}}
								required
							/>
						</div>

						<div className='space-y-2'>
							<Label htmlFor='area_total'>Общая площадь (м²) *</Label>
							<Input
								id='area_total'
								type='number'
								step='0.1'
								min='0'
								placeholder='90'
								value={
									formData.area_total === undefined ? '' : formData.area_total
								}
								onChange={e => {
									const val = e.target.value
									updateField(
										'area_total',
										val === '' ? undefined : parseFloat(val)
									)
								}}
								required
							/>
						</div>

						<div className='space-y-2'>
							<Label htmlFor='area_living'>Жилая площадь (м²)</Label>
							<Input
								id='area_living'
								type='number'
								step='0.1'
								min='0'
								placeholder='65'
								value={
									formData.area_living === undefined ? '' : formData.area_living
								}
								onChange={e => {
									const val = e.target.value
									updateField(
										'area_living',
										val === '' ? undefined : parseFloat(val)
									)
								}}
							/>
						</div>

						<div className='space-y-2'>
							<Label htmlFor='area_kitchen'>Площадь кухни (м²)</Label>
							<Input
								id='area_kitchen'
								type='number'
								step='0.1'
								min='0'
								placeholder='20'
								value={
									formData.area_kitchen === undefined
										? ''
										: formData.area_kitchen
								}
								onChange={e => {
									const val = e.target.value
									updateField(
										'area_kitchen',
										val === '' ? undefined : parseFloat(val)
									)
								}}
							/>
						</div>

						<div className='space-y-2'>
							<Label htmlFor='floor'>Этаж</Label>
							<Input
								id='floor'
								type='number'
								min='1'
								placeholder='24'
								value={formData.floor === undefined ? '' : formData.floor}
								onChange={e => {
									const val = e.target.value
									updateField('floor', val === '' ? undefined : parseInt(val))
								}}
							/>
						</div>

						<div className='space-y-2'>
							<Label htmlFor='floors_total'>Этажей в доме</Label>
							<Input
								id='floors_total'
								type='number'
								min='1'
								placeholder='31'
								value={
									formData.floors_total === undefined
										? ''
										: formData.floors_total
								}
								onChange={e => {
									const val = e.target.value
									updateField(
										'floors_total',
										val === '' ? undefined : parseInt(val)
									)
								}}
							/>
						</div>

						<div className='space-y-2'>
							<Label htmlFor='year_built'>Год постройки</Label>
							<Input
								id='year_built'
								type='number'
								min='1800'
								max={new Date().getFullYear()}
								placeholder='2008'
								value={
									formData.year_built === undefined ? '' : formData.year_built
								}
								onChange={e => {
									const val = e.target.value
									updateField(
										'year_built',
										val === '' ? undefined : parseInt(val)
									)
								}}
							/>
						</div>

						<div className='space-y-2'>
							<Label htmlFor='house_material'>Материал дома</Label>
							<Input
								id='house_material'
								placeholder='монолит'
								value={formData.house_material || ''}
								onChange={e => {
									const val = e.target.value.trim()
									updateField('house_material', val === '' ? undefined : val)
								}}
							/>
						</div>

						<div className='space-y-2'>
							<Label htmlFor='condition'>Состояние</Label>
							<Select
								value={formData.condition || ''}
								onValueChange={value =>
									updateField('condition', value === '' ? undefined : value)
								}
							>
								<SelectTrigger>
									<SelectValue placeholder='Выберите состояние' />
								</SelectTrigger>
								<SelectContent>
									{CONDITION_OPTIONS.map(option => (
										<SelectItem key={option} value={option}>
											{option}
										</SelectItem>
									))}
								</SelectContent>
							</Select>
						</div>
					</div>

					<div className='flex items-center justify-between p-4 rounded-lg border bg-muted/50'>
						<div className='space-y-0.5'>
							<Label
								htmlFor='with_text'
								className='text-base font-medium cursor-pointer'
							>
								Текстовый анализ с AI
							</Label>
							<p className='text-sm text-muted-foreground'>
								Добавить развернутое описание и факторы оценки
							</p>
						</div>
						<Switch
							id='with_text'
							checked={withText}
							onCheckedChange={handleWithTextChange}
						/>
					</div>

					<Button type='submit' className='w-full' disabled={loading}>
						{loading ? (
							<>
								<Loader2 className='mr-2 h-4 w-4 animate-spin' />
								Обработка...
							</>
						) : (
							'Рассчитать стоимость'
						)}
					</Button>
				</form>
			</CardContent>
		</Card>
	)
}