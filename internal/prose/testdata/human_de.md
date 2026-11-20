## Geschichte

Ausgehend von Problemen bei der Verarbeitung von Daten in einfachen Dateien wurde in den 1960er Jahren das Konzept eingeführt, Daten durch eine separate Softwareschicht zwischen Betriebssystem (Dateiverwaltung) und Anwendungsprogramm zu verwalten. Dieses Konzept begegnete der Fehlentwicklung, dass Datenspeicher in Form von Dateien in der Regel für eine spezielle Anwendung konzipiert wurden und ein erheblicher Teil des Tagesgeschäfts mit Umkopieren, Mischen und Restrukturieren der Dateien belastet war.

Eines der ersten großen DBMS war IMS mit der Sprache DL/I (Data Language One). Die damit verwalteten Datenbanken waren hierarchisch strukturiert. Parallel dazu definierte CODASYL ein Modell für netzwerkartig strukturierte Datenbanken.

Einen wesentlichen Fortschritt erzielte in den 1960er und 1970er Jahren Edgar F. Codd mit seiner Forschungsarbeit am IBM Almaden Research Center. Codd entwickelte die Grundlagen des ersten experimentellen relationalen Datenbanksystems System R. Die Berkeley Group folgte mit Ingres und der Abfragesprache QUEL.

Oracle (damals noch unter den Firmennamen SDL und RSI) verwertete die Ergebnisse des System R und führte SQL zum kommerziellen Erfolg. IBM folgte mit SQL/DS und DB2. Die relationalen Datenbanksysteme verdrängten in den 1980er Jahren die hierarchischen und netzwerkartigen Systeme und der Großteil der Behörden, Konzerne, Institute und mittelständischen Unternehmen stellte seine IT auf Datenbanksysteme um.

Während in den 1990er Jahren wenige kommerzielle Hersteller von Datenbank-Software faktisch den Markt beherrschten (namentlich IBM, Informix, dBASE, Microsoft SQL Server und Oracle), erlangten in den 2000ern die Open-Source-Datenbankmanagementsysteme eine immer größere Bedeutung. Vor allem MySQL und PostgreSQL erzielten signifikante Marktanteile. Als Reaktion begannen die führenden kommerziellen Hersteller, gebührenfreie Versionen ihrer Datenbank-Software anzubieten. Etwa seit 2001 ist aufgrund mangelnder Skalierbarkeit relationaler Datenbanken die Bedeutung der NoSQL-Systeme gewachsen.

Einen Stammbaum der Datenbanksysteme findet man als Genealogy of Relational Database Management Systems beim Hasso-Plattner-Institut.

## Bedeutung

Datenbanksysteme sind heute ein zentraler Bestandteil der Unternehmenssoftware. Damit stellen sie einen kritischen Teil vieler Unternehmen und Behörden dar. Von der Verfügbarkeit, Vollständigkeit und Richtigkeit der Daten hängt die Aktionsfähigkeit eines Unternehmens ab. Die Datensicherheit ist daher ein wichtiger und gesetzlich vorgeschriebener Bestandteil der IT eines Unternehmens oder einer Behörde.

## Komponenten eines Datenbanksystems

Das Datenbanksystem ist das ausgeführte DBMS zusammen mit den zu verwaltenden Daten der Datenbank. Eine Datenbank gewährleistet die persistente Speicherung sowie die Konsistenz der Nutzdaten einer Institution und bietet für die benutzenden Datenbankanwendungen mit dem DBMS Schnittstellen zur Abfrage, Auswertung, Veränderung und Verwaltung dieser Daten.

### Datenbankmanagementsystem

Das Datenbankmanagementsystem (DBMS) ist die eingesetzte Software, die für das Datenbanksystem installiert und konfiguriert wird. Das DBMS legt das Datenbankmodell fest, hat einen Großteil der unten angeführten Anforderungen zu sichern und entscheidet maßgeblich über Funktionalität und Geschwindigkeit des Systems. Datenbankmanagementsysteme selbst sind hochkomplexe Softwaresysteme.

Für Datenbankmanagementsystem wird (selten) auch der Begriff Datenbankverwaltungssystem (DBVS) verwendet.

Gängig ist die Abkürzung RDBMS für ein relationales Datenbankmanagementsystem.

### Datenbank

In der Theorie versteht man unter Datenbank (engl. database) einen logisch zusammengehörigen Datenbestand. Dieser Datenbestand wird von einem laufenden DBMS verwaltet und für Anwendungssysteme und Benutzer unsichtbar auf nichtflüchtigen Speichermedien abgelegt. Um einen effizienten Zugriff auf die Datenbank zu gewährleisten, verwaltet das DBMS in der Regel eine Speicherhierarchie, die insbesondere auch einen schnellen Zwischenspeicher (Pufferpool) umfasst. Zur Wahrung der Konsistenz des Datenbestandes müssen sich alle Anwendungssysteme an das DBMS wenden, um die Datenbank nutzen zu können. Allein administrativen Tätigkeiten, wie zum Beispiel der Datensicherung, ist der direkte Zugriff auf den Speicher erlaubt. Die logische Struktur der zu speichernden Daten wird bei der Datenmodellierung als Datenmodell erarbeitet und festgelegt und in ihrer finalen Form im und nach den Syntaxregeln des DBMS gespeichert. Hierzu erzeugt, nutzt und verwaltet das DBMS einen „Systemkatalog“ (Data-Dictionary) mit Metainformationen zum Datenbestand, beispielsweise über seine Struktur, seine Datenfelder (Name, Länge, Format …), Zugriffsregeln, Integritätsbedingungen usw.

Einzelne DBMS-Hersteller verwenden geringfügig voneinander abweichende Begrifflichkeiten dafür, was man genau unter einer Datenbank versteht: entweder alle Daten, die von einem laufenden DBMS bzw. der Instanz verwaltet werden, oder nur die jeweils inhaltlich zusammengehörigen Daten. Bei verteilten Datenbanken gibt es auch im Modell mehrere Datenbanken auf unterschiedlichen Systemen, die miteinander verbunden sind.
