async function getNumberFormatLocale() {
    try {
        const response = await fetch('/api/config'); 
        const config = await response.json();
        
        console.log(config.NumberFormatLocale); 
    } catch (error) {
        console.error('Error fetching the config:', error);
    }
}

export function NumberFormatLocale(number) {
    if (getNumberFormatLocale() == "Brazil") {
        if (!number.startsWith('55')) {
            number = '55' + number;
        }
    
        if (number.length === 13 && number[4] === '9') {
            return number.slice(0, 4) + number.slice(5);
        }
        return number;
    }
    return number;
}

